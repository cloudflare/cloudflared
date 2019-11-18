package pogs

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/stretchr/testify/assert"
	capnp "zombiezen.com/go/capnproto2"
)

// Ensure the AuthOutcome sum is correct
var _ AuthOutcome = &AuthSuccess{}
var _ AuthOutcome = &AuthFail{}
var _ AuthOutcome = &AuthUnknown{}

// Unit tests for AuthenticateResponse.Outcome()
func TestAuthenticateResponseOutcome(t *testing.T) {
	type fields struct {
		PermanentErr      string
		RetryableErr      string
		Jwt               []byte
		HoursUntilRefresh uint8
	}
	tests := []struct {
		name   string
		fields fields
		want   AuthOutcome
	}{
		{"success",
			fields{Jwt: []byte("asdf"), HoursUntilRefresh: 6},
			&AuthSuccess{Jwt: []byte("asdf"), HoursUntilRefresh: 6},
		},
		{"fail",
			fields{PermanentErr: "bad creds"},
			&AuthFail{Err: fmt.Errorf("bad creds")},
		},
		{"error",
			fields{RetryableErr: "bad conn", HoursUntilRefresh: 6},
			&AuthUnknown{Err: fmt.Errorf("bad conn"), HoursUntilRefresh: 6},
		},
		{"nil (no fields are set)",
			fields{},
			nil,
		},
		{"nil (too few fields are set)",
			fields{HoursUntilRefresh: 6},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := AuthenticateResponse{
				PermanentErr:      tt.fields.PermanentErr,
				RetryableErr:      tt.fields.RetryableErr,
				Jwt:               tt.fields.Jwt,
				HoursUntilRefresh: tt.fields.HoursUntilRefresh,
			}
			if got := ar.Outcome(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AuthenticateResponse.Outcome() = %T, want %v", got, tt.want)
			}
		})
	}
}

func TestWhenToRefresh(t *testing.T) {
	expected := 4 * time.Hour
	actual := hoursToTime(4)
	if expected != actual {
		t.Fatalf("expected %v hours, got %v", expected, actual)
	}
}

// Test that serializing and deserializing AuthenticationResponse undo each other.
func TestSerializeAuthenticationResponse(t *testing.T) {

	tests := []*AuthenticateResponse{
		&AuthenticateResponse{
			Jwt:               []byte("\xbd\xb2\x3d\xbc\x20\xe2\x8c\x98"),
			HoursUntilRefresh: 24,
		},
		&AuthenticateResponse{
			PermanentErr: "bad auth",
		},
		&AuthenticateResponse{
			RetryableErr:      "bad connection",
			HoursUntilRefresh: 24,
		},
	}

	for i, testCase := range tests {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewAuthenticateResponse(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalAuthenticateResponse(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalAuthenticateResponse(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}
