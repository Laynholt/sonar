package httpx

import "testing"

func TestStreamClientHasNoTotalTimeout(t *testing.T) {
	if StreamClient().Timeout != 0 {
		t.Errorf("StreamClient timeout = %v, want none", StreamClient().Timeout)
	}
	if Client().Timeout == 0 || StreamClient() == Client() {
		t.Error("Client must keep its own timeout and stay a separate instance")
	}
}
