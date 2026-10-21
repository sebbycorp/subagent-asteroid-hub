package web

import "testing"

func TestEmbedsAgentgatewayLogo(t *testing.T) {
	b, err := Files.ReadFile("agentgateway-mark.svg")
	if err != nil || len(b) == 0 {
		t.Fatalf("agentgateway-mark.svg not embedded: %v", err)
	}
}

func TestEmbedsKagentLogo(t *testing.T) {
	if b, err := Files.ReadFile("kagent-mark.png"); err != nil || len(b) == 0 {
		t.Fatalf("kagent-mark.png not embedded: %v", err)
	}
}
