package pogs

import "testing"

func TestScopeUnmarshaler_UnmarshalJSON(t *testing.T) {
	type fields struct {
		Scope Scope
	}
	type args struct {
		b []byte
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		wantErr   bool
		wantScope Scope
	}{
		{
			name:      "group_successful",
			args:      args{b: []byte(`{"group": "my-group"}`)},
			wantScope: NewGroup("my-group"),
		},
		{
			name:      "system_name_successful",
			args:      args{b: []byte(`{"system_name": "my-computer"}`)},
			wantScope: NewSystemName("my-computer"),
		},
		{
			name:    "not_a_scope",
			args:    args{b: []byte(`{"x": "y"}`)},
			wantErr: true,
		},
		{
			name:    "malformed_group",
			args:    args{b: []byte(`{"group": ["a", "b"]}`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			su := &ScopeUnmarshaler{
				Scope: tt.fields.Scope,
			}
			err := su.UnmarshalJSON(tt.args.b)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ScopeUnmarshaler.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !eqScope(tt.wantScope, su.Scope) {
					t.Errorf("Wanted scope %v but got scope %v", tt.wantScope, su.Scope)
				}
			}
		})
	}
}

func eqScope(s1, s2 Scope) bool {
	return s1.Value() == s2.Value() && s1.PostgresType() == s2.PostgresType()
}
