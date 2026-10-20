package nugetfetch

import (
	"strings"
	"testing"
)

func TestValidateAcceptsRealRequests(t *testing.T) {
	good := []Request{
		{ID: "Newtonsoft.Json", Version: "13.0.3", Framework: "net8.0"},
		{ID: "My_Corp-Lib.Core", Version: "1.2.3-beta.1", Framework: "net10.0"},
		{ID: "A", Version: "1.0.0.4", Framework: "netstandard2.0"},
		{ID: "A", Version: "2020.01.01", Framework: "net48"},
		{ID: "A", Version: "1.0.0+build.5", Framework: "netcoreapp3.1"},
	}
	for _, r := range good {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", r, err)
		}
	}
}

func TestValidateRejectsAnythingThatCouldBeMoreThanData(t *testing.T) {
	bad := map[string]Request{
		"empty id":            {ID: "", Version: "1.0.0", Framework: "net8.0"},
		"id with a quote":     {ID: `A"/><Target Name="x"`, Version: "1.0.0", Framework: "net8.0"},
		"id with a slash":     {ID: "../A", Version: "1.0.0", Framework: "net8.0"},
		"id with a space":     {ID: "A B", Version: "1.0.0", Framework: "net8.0"},
		"id too long":         {ID: strings.Repeat("a", 101), Version: "1.0.0", Framework: "net8.0"},
		"version range":       {ID: "A", Version: "[1.0.0,2.0.0)", Framework: "net8.0"},
		"floating version":    {ID: "A", Version: "1.*", Framework: "net8.0"},
		"empty version":       {ID: "A", Version: "", Framework: "net8.0"},
		"version with markup": {ID: "A", Version: "1.0.0</Version>", Framework: "net8.0"},
		"empty framework":     {ID: "A", Version: "1.0.0", Framework: ""},
		"unknown framework":   {ID: "A", Version: "1.0.0", Framework: "net8.0;evil"},
		"framework with path": {ID: "A", Version: "1.0.0", Framework: "../net8.0"},
	}
	for name, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: %+v accepted", name, r)
		}
	}
}

func TestValidateErrorSaysWhichFieldAndWhat(t *testing.T) {
	err := Request{ID: "A", Version: "1.*", Framework: "net8.0"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "version") || !strings.Contains(err.Error(), "1.*") {
		t.Errorf("error should name the version and its value: %v", err)
	}
}
