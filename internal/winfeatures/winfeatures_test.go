package winfeatures

import (
	"strings"
	"testing"
)

const infoOutput = "\r\nDeployment Image Servicing and Management tool\r\nVersion: 10.0.26100.1150\r\n\r\n" +
	"Image Version: 10.0.26100.2033\r\n\r\nFeature Information:\r\n\r\n" +
	"Feature Name : VirtualMachinePlatform\r\nDisplay Name : Virtual Machine Platform\r\n" +
	"Description : Enables platform support for virtual machines\r\nRestart Required : Possible\r\n" +
	"State : %s\r\n\r\nCustom Properties:\r\n\r\n(No custom properties found)\r\n\r\nThe operation completed successfully.\r\n"

func TestParseStateReadsTheStateLine(t *testing.T) {
	for _, state := range []string{"Enabled", "Disabled", "Enable Pending", "Disabled with Payload Removed"} {
		got, ok := ParseState(strings.Replace(infoOutput, "%s", state, 1))
		if !ok || got != state {
			t.Errorf("ParseState(... State : %s ...) = %q, %v", state, got, ok)
		}
	}
}

func TestParseStateWithoutAStateLine(t *testing.T) {
	if got, ok := ParseState("Error: 740\r\n\r\nElevated permissions are required to run DISM.\r\n"); ok {
		t.Errorf("ParseState found state %q in output without one", got)
	}
}

func TestEnabledAndPending(t *testing.T) {
	if !Enabled("Enabled") || Enabled("Enable Pending") || Enabled("Disabled") {
		t.Error("Enabled should hold only for Enabled")
	}
	if !Pending("Enable Pending") || Pending("Enabled") || Pending("Disable Pending") {
		t.Error("Pending should hold only for Enable Pending")
	}
}

func TestEnableArgsNamesEveryFeature(t *testing.T) {
	got := CommandLine(EnableArgs(Required))
	want := "dism /Online /English /Enable-Feature /FeatureName:HypervisorPlatform /FeatureName:VirtualMachinePlatform /All /NoRestart"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestOutcome(t *testing.T) {
	for _, tt := range []struct {
		code        int
		ok, restart bool
	}{
		{0, true, false},
		{3010, true, true},
		{740, false, false},
		{1223, false, false},
		{-2146498548, false, false}, // 0x800f080c: unknown feature
	} {
		ok, restart, why := Outcome(tt.code)
		if ok != tt.ok || restart != tt.restart || ok == (why != "") {
			t.Errorf("Outcome(%d) = %v, %v, %q", tt.code, ok, restart, why)
		}
	}
	if _, _, why := Outcome(-2146498548); !strings.Contains(why, "0x800f080c") {
		t.Errorf("an HRESULT should be shown in hex, got %q", why)
	}
}
