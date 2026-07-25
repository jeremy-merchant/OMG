//go:build darwin

// Package unixacl contains descriptor-based extended ACL guards shared by
// sensitive Unix file consumers.
package unixacl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	darwinAttrBitMapCount            = 5
	darwinAttrCommonExtendedSecurity = 0x00400000
	darwinFilesecNoACL               = ^uint32(0)
	darwinACLMaxEntries              = 128
	darwinACLPermit                  = 1
	darwinACLDeny                    = 2
	darwinACLKindMask                = 0xf
	darwinACLHeaderSize              = 44
	darwinACLEntrySize               = 24
	darwinACEFlagsOffset             = 16
	darwinACERightsOffset            = 20

	darwinACLGenericAll   = 1 << 21
	darwinACLGenericWrite = 1 << 23
	darwinACLGenericRead  = 1 << 24

	darwinPayloadDangerousRights = (1 << 1) | (1 << 2) | (1 << 4) | (1 << 5) | (1 << 12) | (1 << 13) | darwinACLGenericAll | darwinACLGenericWrite | darwinACLGenericRead
	darwinStateDangerousRights   = (1 << 2) | (1 << 4) | (1 << 5) | (1 << 6) | (1 << 12) | (1 << 13) | darwinACLGenericAll | darwinACLGenericWrite
)

type darwinAttrList struct {
	BitMapCount uint16
	Reserved    uint16
	CommonAttr  uint32
	VolAttr     uint32
	DirAttr     uint32
	FileAttr    uint32
	ForkAttr    uint32
}

var (
	darwinCurrentUserGUIDOnce sync.Once
	darwinCurrentUserGUID     [16]byte
	darwinCurrentUserGUIDErr  error
)

func currentDarwinUserGUID() ([16]byte, error) {
	darwinCurrentUserGUIDOnce.Do(func() {
		account, err := user.Current()
		if err != nil {
			darwinCurrentUserGUIDErr = err
			return
		}
		output, err := exec.Command("/usr/bin/dsmemberutil", "getuuid", "-U", account.Username).Output()
		if err != nil {
			darwinCurrentUserGUIDErr = err
			return
		}
		darwinCurrentUserGUID, darwinCurrentUserGUIDErr = parseDarwinUUID(strings.TrimSpace(string(output)))
	})
	return darwinCurrentUserGUID, darwinCurrentUserGUIDErr
}

func parseDarwinUUID(value string) ([16]byte, error) {
	var guid [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return guid, errors.New("invalid Darwin user UUID")
	}
	compact := strings.ReplaceAll(value, "-", "")
	if _, err := hex.Decode(guid[:], []byte(compact)); err != nil {
		return guid, errors.New("invalid Darwin user UUID")
	}
	for left, right := 0, 3; left < right; left, right = left+1, right-1 {
		guid[left], guid[right] = guid[right], guid[left]
	}
	for left, right := 4, 5; left < right; left, right = left+1, right-1 {
		guid[left], guid[right] = guid[right], guid[left]
	}
	for left, right := 6, 7; left < right; left, right = left+1, right-1 {
		guid[left], guid[right] = guid[right], guid[left]
	}
	return guid, nil
}

func darwinGUIDMatches(ace []byte, guid [16]byte) bool {
	if bytes.Equal(ace, guid[:]) {
		return true
	}
	for left, right := 0, 3; left < right; left, right = left+1, right-1 {
		guid[left], guid[right] = guid[right], guid[left]
	}
	guid[4], guid[5] = guid[5], guid[4]
	for left, right := 6, 7; left < right; left, right = left+1, right-1 {
		guid[left], guid[right] = guid[right], guid[left]
	}
	return bytes.Equal(ace, guid[:])
}

// RejectPayloadACLFD rejects effective ACL grants that can read, mutate, or
// take ownership of a private payload. Harmless metadata grants are allowed.
func RejectPayloadACLFD(fd int) error {
	return rejectExtendedACLFD(fd, darwinPayloadDangerousRights)
}

// RejectStateAncestorACLFD rejects effective ACL grants that can create,
// replace, delete, or take ownership of state below a directory.
func RejectStateAncestorACLFD(fd int) error {
	return rejectExtendedACLFD(fd, darwinStateDangerousRights)
}

func rejectExtendedACLFD(fd int, dangerousRights uint32) error {
	var attributes darwinAttrList
	attributes.BitMapCount = darwinAttrBitMapCount
	attributes.CommonAttr = darwinAttrCommonExtendedSecurity
	var result [4096]byte
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, uintptr(fd), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&result[0])), uintptr(len(result)), 0, 0)
	if errno != 0 {
		return errno
	}
	if len(result) < 12 {
		return errors.New("invalid extended ACL response")
	}
	offset := int(int32(binary.LittleEndian.Uint32(result[4:8])))
	length := int(binary.LittleEndian.Uint32(result[8:12]))
	start := 4 + offset
	const aclEntryCountOffset = 36
	if length == 0 {
		return nil
	}
	if start < 0 || start > len(result) || length > len(result)-start || length < darwinACLHeaderSize {
		return errors.New("invalid extended ACL response")
	}
	entryCount := binary.LittleEndian.Uint32(result[start+aclEntryCountOffset : start+aclEntryCountOffset+4])
	if entryCount == darwinFilesecNoACL {
		return nil
	}
	if entryCount > darwinACLMaxEntries || length != darwinACLHeaderSize+int(entryCount)*darwinACLEntrySize {
		return errors.New("invalid extended ACL response")
	}
	for entry := range int(entryCount) {
		entryOffset := start + darwinACLHeaderSize + entry*darwinACLEntrySize
		kind := binary.LittleEndian.Uint32(result[entryOffset+darwinACEFlagsOffset:entryOffset+darwinACEFlagsOffset+4]) & darwinACLKindMask
		if kind == darwinACLDeny {
			continue
		}
		rights := binary.LittleEndian.Uint32(result[entryOffset+darwinACERightsOffset : entryOffset+darwinACERightsOffset+4])
		if kind != darwinACLPermit {
			return errors.New("extended ACL prevents an owner-only guarantee")
		}
		if rights&dangerousRights != 0 {
			ownerGUID, err := currentDarwinUserGUID()
			if err != nil || !darwinGUIDMatches(result[entryOffset:entryOffset+16], ownerGUID) {
				return errors.New("extended ACL prevents an owner-only guarantee")
			}
		}
	}
	return nil
}
