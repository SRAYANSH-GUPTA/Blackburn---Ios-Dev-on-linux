package cli

import (
	"blackburn/internal/config"
	"strings"
	"testing"
)

func TestSimulatorInteractionValidation(t *testing.T) {
	for _, args := range [][]string{
		{"stream", "--port=0"}, {"stream", "--port=65536"}, {"stream", "--fps=0"}, {"stream", "--fps=61"}, {"stream", "--scale=4"}, {"stream", "--wat"}, {"stream", "extra"},
		{"touch", "NaN", "1"}, {"touch", "-1", "1"}, {"touch", "1"}, {"touch", "1", "Infinity"}, {"tap", "--evil"}, {"key"}, {"text", ""},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			a, _, calls := testApp(t)
			if exit := a.Main(append([]string{"sim"}, args...)); exit != 2 {
				t.Fatal(exit, a.Err)
			}
			if len(*calls) != 0 {
				t.Fatal("provider invoked for invalid command", *calls)
			}
		})
	}
}
func TestSimulatorInteractionsUseLeasedDevice(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"touch", "12", "34"}, "ad press 12.000 34.000"},
		{[]string{"tap", "label=Save"}, "ad click label=Save"},
		{[]string{"key", "home"}, "ad home"},
		{[]string{"text", "hello world"}, "ad type hello world"},
	} {
		a, _, calls := testApp(t)
		c := config.Defaults()
		if err := a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"], Device: "SIM-1", Stage: "SIM_READY"}); err != nil {
			t.Fatal(err)
		}
		if exit := a.Main(append([]string{"sim"}, tc.args...)); exit != 0 {
			t.Fatal(exit, a.Err)
		}
		if len(*calls) != 1 || !strings.HasSuffix(strings.Join((*calls)[0], " "), tc.want) {
			t.Fatal(*calls)
		}
	}
}
func TestSimulatorInteractionRequiresLease(t *testing.T) {
	a, _, calls := testApp(t)
	if exit := a.Main([]string{"sim", "stream"}); exit != 3 {
		t.Fatal(exit, a.Err)
	}
	if len(*calls) != 0 {
		t.Fatal("provider invoked without lease")
	}
}
