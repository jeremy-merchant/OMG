package lifecycle

import "testing"

func TestClassifyProportionalModes(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		mode  Mode
		level VerificationLevel
	}{
		{name: "read only", input: Input{}, mode: Observe, level: VerificationNone},
		{name: "small file edit", input: Input{MutatesFiles: true}, mode: WorkLite, level: VerificationLow},
		{name: "visible edit", input: Input{MutatesFiles: true, ChangesUserVisibleBehavior: true}, mode: WorkLite, level: VerificationMedium},
		{name: "multi agent", input: Input{UsesMultipleAgents: true}, mode: Full, level: VerificationMedium},
		{name: "release", input: Input{ReleaseOrCanary: true}, mode: Full, level: VerificationHigh},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Classify(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Mode != test.mode || got.VerificationLevel != test.level {
				t.Fatalf("contract = %+v", got)
			}
		})
	}
}

func TestClassifyLifecycleRequirements(t *testing.T) {
	observe, _ := Classify(Input{})
	if observe.SessionRequired || observe.TaskRequired || observe.RunRequired || observe.HandoffRequired || !observe.AutoArchive {
		t.Fatalf("observe contract = %+v", observe)
	}

	lite, _ := Classify(Input{MutatesFiles: true, ExpectedDurationMinutes: 20})
	if !lite.SessionRequired || !lite.TaskRequired || !lite.RunRequired || !lite.ProgressRequired || !lite.ReservationRequired || lite.HandoffRequired || !lite.AutoArchive {
		t.Fatalf("work-lite contract = %+v", lite)
	}

	full, _ := Classify(Input{RequiresHandoff: true})
	if !full.HandoffRequired || !full.IndependentVerificationRequired || full.AutoArchive {
		t.Fatalf("full contract = %+v", full)
	}
}

func TestClassifyRejectsInvalidInputAndUnsafeDowngrade(t *testing.T) {
	if _, err := Classify(Input{ExpectedDurationMinutes: -1}); err == nil {
		t.Fatal("negative duration accepted")
	}
	if _, err := Classify(Input{Override: "unknown"}); err == nil {
		t.Fatal("unknown override accepted")
	}
	got, err := Classify(Input{TouchesProduction: true, Override: "observe"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != Full || got.Reasons[len(got.Reasons)-1] != "unsafe_downgrade_ignored" {
		t.Fatalf("unsafe downgrade contract = %+v", got)
	}
}
