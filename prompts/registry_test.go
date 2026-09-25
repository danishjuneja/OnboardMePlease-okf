package prompts

import "testing"

func TestPackagedRegistry(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
}
