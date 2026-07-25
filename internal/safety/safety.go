// Package safety centralizes rules that keep OMG bearer credentials out of canonical state and default output.
package safety

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	delegationTokenPrefix = "omgdt_v1_"
	delegationTokenLength = 43 // Raw URL-safe base64 encoding of 256 random bits.
	redactedToken         = "[REDACTED:OMG_DELEGATION_TOKEN]"
)

var (
	exactTokenCandidate = regexp.MustCompile(delegationTokenPrefix + `[A-Za-z0-9_-]{` + "43" + `}`)
	ErrDelegationToken  = errors.New("delegation token not permitted")
)

// IsDelegationToken reports whether value contains a complete OMG delegation
// bearer token. It intentionally recognizes only the versioned format, not
// arbitrary base64-looking values.
func IsDelegationToken(value string) bool {
	return exactTokenCandidate.MatchString(value)
}

// Redact removes every complete OMG delegation token from a presentation value.
func Redact(value string) string {
	indexes := exactTokenCandidate.FindAllStringIndex(value, -1)
	if len(indexes) == 0 {
		return value
	}
	var out []byte
	start := 0
	for _, index := range indexes {
		if out == nil {
			out = make([]byte, 0, len(value))
		}
		out = append(out, value[start:index[0]]...)
		out = append(out, redactedToken...)
		start = index[1]
	}
	if out == nil {
		return value
	}
	out = append(out, value[start:]...)
	return string(out)
}

// SafeText normalizes user-controlled presentation text and replaces values
// that look like credentials or private filesystem locations with a stable,
// non-reversible fingerprint.
func SafeText(value string) string {
	if value == "" {
		return ""
	}
	redacted := func() string {
		sum := sha256.Sum256([]byte(value))
		return "[REDACTED:" + hex.EncodeToString(sum[:8]) + "]"
	}
	if !utf8.ValidString(value) {
		return redacted()
	}
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	normalized := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(normalized) > 512 {
		return redacted()
	}
	if domain.ContainsSensitiveStableMetadata(normalized) {
		return redacted()
	}
	if IsDelegationToken(normalized) {
		return Redact(normalized)
	}
	return normalized
}

// Reject recursively checks every string and byte slice that can be written.
// It streams ordered composite string leaves through a bounded token matcher,
// so a bearer credential cannot be reconstructed by splitting it across fields.
func Reject(value any) error {
	scanner := tokenScanner{}
	if containsToken(reflect.ValueOf(value), make(map[visit]struct{}), &scanner) {
		return ErrDelegationToken
	}
	return nil
}

// RejectPrefixed scans prefix and payload as one ordered stream of reachable
// text leaves. It also retains the established prefix-to-single-leaf check so
// unrelated metadata before a payload leaf cannot hide a credential.
func RejectPrefixed(prefix any, payload ...any) error {
	if Reject(prefix) != nil || Reject(payload) != nil {
		return ErrDelegationToken
	}
	prefixScanner := tokenScanner{}
	if containsToken(reflect.ValueOf(prefix), make(map[visit]struct{}), &prefixScanner) {
		return ErrDelegationToken
	}
	for _, value := range payload {
		if containsStringLeaf(reflect.ValueOf(value), make(map[visit]struct{}), func(leaf reflect.Value) bool {
			candidate := prefixScanner
			return scanLeaf(&candidate, leaf)
		}) {
			return ErrDelegationToken
		}
	}

	scanner := tokenScanner{}
	seen := make(map[visit]struct{})
	if containsToken(reflect.ValueOf(prefix), seen, &scanner) {
		return ErrDelegationToken
	}
	for _, value := range payload {
		if containsToken(reflect.ValueOf(value), seen, &scanner) {
			return ErrDelegationToken
		}
	}
	return nil
}

type tokenScanner struct {
	prefixLength  int
	payloadLength int
}

func (s *tokenScanner) scan(value string) bool {
	for i := range len(value) {
		character := value[i]
		if s.prefixLength < len(delegationTokenPrefix) {
			if character == delegationTokenPrefix[s.prefixLength] {
				s.prefixLength++
				continue
			}
			s.restart(character)
			continue
		}
		if isDelegationTokenCharacter(character) {
			s.payloadLength++
			if s.payloadLength == delegationTokenLength {
				return true
			}
			continue
		}
		s.restart(character)
	}
	return false
}

func (s *tokenScanner) scanBytes(value []byte) bool {
	for _, character := range value {
		if s.prefixLength < len(delegationTokenPrefix) {
			if character == delegationTokenPrefix[s.prefixLength] {
				s.prefixLength++
				continue
			}
			s.restart(character)
			continue
		}
		if isDelegationTokenCharacter(character) {
			s.payloadLength++
			if s.payloadLength == delegationTokenLength {
				return true
			}
			continue
		}
		s.restart(character)
	}
	return false
}

// restart discards an interrupted candidate while retaining the current byte
// when it can begin the next candidate.
func (s *tokenScanner) restart(character byte) {
	s.payloadLength = 0
	if character == delegationTokenPrefix[0] {
		s.prefixLength = 1
		return
	}
	s.prefixLength = 0
}

func isDelegationTokenCharacter(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' || value == '-'
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

func containsToken(value reflect.Value, seen map[visit]struct{}, scanner *tokenScanner) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Interface:
		return !value.IsNil() && containsToken(value.Elem(), seen, scanner)
	case reflect.String:
		return scanner.scan(value.String())
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return scanner.scanBytes(value.Bytes())
		}
		if value.IsNil() {
			return false
		}
		key, repeated := enter(value, seen)
		if repeated {
			return false
		}
		defer leave(key, seen)
		for i := range value.Len() {
			if containsToken(value.Index(i), seen, scanner) {
				return true
			}
		}
	case reflect.Array:
		for i := range value.Len() {
			if containsToken(value.Index(i), seen, scanner) {
				return true
			}
		}
	case reflect.Ptr:
		if value.IsNil() {
			return false
		}
		key, repeated := enter(value, seen)
		if repeated {
			return false
		}
		defer leave(key, seen)
		return containsToken(value.Elem(), seen, scanner)
	case reflect.Map:
		if value.IsNil() {
			return false
		}
		key, repeated := enter(value, seen)
		if repeated {
			return false
		}
		defer leave(key, seen)
		for _, mapKey := range sortedMapKeys(value) {
			if containsToken(mapKey, seen, scanner) || containsToken(value.MapIndex(mapKey), seen, scanner) {
				return true
			}
		}
	case reflect.Struct:
		for i := range value.NumField() {
			if value.Type().Field(i).PkgPath == "" && containsToken(value.Field(i), seen, scanner) {
				return true
			}
		}
	}
	return false
}

func scanLeaf(scanner *tokenScanner, value reflect.Value) bool {
	if value.Kind() == reflect.String {
		return scanner.scan(value.String())
	}
	return scanner.scanBytes(value.Bytes())
}

func containsStringLeaf(value reflect.Value, seen map[visit]struct{}, visitLeaf func(reflect.Value) bool) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Interface:
		return !value.IsNil() && containsStringLeaf(value.Elem(), seen, visitLeaf)
	case reflect.String:
		return visitLeaf(value)
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return visitLeaf(value)
		}
		if value.IsNil() {
			return false
		}
		key, repeated := enter(value, seen)
		if repeated {
			return false
		}
		defer leave(key, seen)
		for i := range value.Len() {
			if containsStringLeaf(value.Index(i), seen, visitLeaf) {
				return true
			}
		}
	case reflect.Array:
		for i := range value.Len() {
			if containsStringLeaf(value.Index(i), seen, visitLeaf) {
				return true
			}
		}
	case reflect.Ptr:
		if value.IsNil() {
			return false
		}
		key, repeated := enter(value, seen)
		if repeated {
			return false
		}
		defer leave(key, seen)
		return containsStringLeaf(value.Elem(), seen, visitLeaf)
	case reflect.Map:
		if value.IsNil() {
			return false
		}
		key, repeated := enter(value, seen)
		if repeated {
			return false
		}
		defer leave(key, seen)
		for _, mapKey := range sortedMapKeys(value) {
			if containsStringLeaf(mapKey, seen, visitLeaf) || containsStringLeaf(value.MapIndex(mapKey), seen, visitLeaf) {
				return true
			}
		}
	case reflect.Struct:
		for i := range value.NumField() {
			if value.Type().Field(i).PkgPath == "" && containsStringLeaf(value.Field(i), seen, visitLeaf) {
				return true
			}
		}
	}
	return false
}

func sortedMapKeys(value reflect.Value) []reflect.Value {
	keys := value.MapKeys()
	sort.SliceStable(keys, func(i, j int) bool {
		left, right := mapKeyOrder(keys[i]), mapKeyOrder(keys[j])
		return left < right
	})
	return keys
}

func mapKeyOrder(value reflect.Value) string {
	if value.CanInterface() {
		return value.Type().String() + ":" + fmt.Sprintf("%#v", value.Interface())
	}
	return value.Type().String()
}

func enter(value reflect.Value, seen map[visit]struct{}) (visit, bool) {
	pointer := value.Pointer()
	if pointer == 0 {
		return visit{}, false
	}
	key := visit{typ: value.Type(), ptr: pointer}
	if _, exists := seen[key]; exists {
		return key, true
	}
	seen[key] = struct{}{}
	return key, false
}

func leave(key visit, seen map[visit]struct{}) {
	if key.ptr != 0 {
		delete(seen, key)
	}
}
