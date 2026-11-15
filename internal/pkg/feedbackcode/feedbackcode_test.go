package feedbackcode

import "testing"

func TestSignDeterministic(t *testing.T) {
	got := Sign("secret", "fc_abc", 1758888888, "SN001", "nonce-1")
	want := "Cr5tQVVgFf1g3adnPzR88-XYa8NGm6HQGuV65cCPcsQ"
	if got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
	if !Equal(got, want) {
		t.Fatal("Equal() = false, want true")
	}
	if Equal(got, "different") {
		t.Fatal("Equal() = true for different values")
	}
}
