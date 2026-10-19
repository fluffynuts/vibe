package hostcpu

import (
	"reflect"
	"testing"
)

func TestEveryNumberOfCPUsUpToOneLessThanTheMachineHasIsOffered(t *testing.T) {
	if got, want := Options(4), []int{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Options(4) = %v, want %v", got, want)
	}
	if got := Options(24); len(got) != 23 || got[0] != 1 || got[22] != 23 {
		t.Errorf("Options(24) = %v, want 1 to 23", got)
	}
}

func TestAMachineWithOneOrTwoCPUsCanOnlyBeOfferedOne(t *testing.T) {
	for _, total := range []int{1, 2} {
		if got := Options(total); !reflect.DeepEqual(got, []int{1}) {
			t.Errorf("Options(%d) = %v, want just 1: the machine keeps what it can", total, got)
		}
	}
}

func TestTheMiddleIsHalfTheMachinesCPUsRoundedDown(t *testing.T) {
	for total, want := range map[int]int{24: 12, 16: 8, 8: 4, 7: 3, 4: 2, 3: 1, 2: 1, 1: 1} {
		if got := Default(total); got != want {
			t.Errorf("Default(%d) = %d, want %d", total, got, want)
		}
	}
}

func TestPickStartsOnTheMiddleWhenNothingIsWanted(t *testing.T) {
	options, idx, over := Pick(24, 0)
	if options[idx] != 12 || over {
		t.Errorf("starts on %d (over %v), want 12", options[idx], over)
	}
}

func TestPickStartsOnWhatIsWantedWhenTheMachineCanSpareIt(t *testing.T) {
	for _, want := range []int{1, 6, 23} {
		options, idx, over := Pick(24, want)
		if options[idx] != want || over {
			t.Errorf("want %d: starts on %d (over %v)", want, options[idx], over)
		}
	}
}

func TestPickLowersWhatIsMoreThanTheMachineCanSpareToTheMost(t *testing.T) {
	for _, want := range []int{24, 99} {
		options, idx, over := Pick(24, want)
		if options[idx] != 23 || !over {
			t.Errorf("want %d: starts on %d (over %v), want 23 and over", want, options[idx], over)
		}
	}
}

func TestPickTakesANegativeWantAsNone(t *testing.T) {
	options, idx, over := Pick(8, -3)
	if options[idx] != 4 || over {
		t.Errorf("starts on %d (over %v), want the middle, 4", options[idx], over)
	}
}

func TestTheMachinesCPUsAreCountedByDefault(t *testing.T) {
	if Total() < 1 {
		t.Errorf("Total() = %d", Total())
	}
}
