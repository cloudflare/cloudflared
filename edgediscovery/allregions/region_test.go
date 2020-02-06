package allregions

import (
	"fmt"
	"net"
	"reflect"
	"testing"
)

func TestRegion_New(t *testing.T) {
	r := NewRegion([]*net.TCPAddr{&addr0, &addr1, &addr2})
	fmt.Println(r.connFor)
	if r.AvailableAddrs() != 3 {
		t.Errorf("r.AvailableAddrs() == %v but want 3", r.AvailableAddrs())
	}
}

func TestRegion_AddrUsedBy(t *testing.T) {
	type fields struct {
		connFor map[*net.TCPAddr]UsedBy
	}
	type args struct {
		connID int
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *net.TCPAddr
	}{
		{
			name: "happy trivial test",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
			}},
			args: args{connID: 0},
			want: &addr0,
		},
		{
			name: "sad trivial test",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
			}},
			args: args{connID: 1},
			want: nil,
		},
		{
			name: "sad test",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			}},
			args: args{connID: 3},
			want: nil,
		},
		{
			name: "happy test",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			}},
			args: args{connID: 1},
			want: &addr1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Region{
				connFor: tt.fields.connFor,
			}
			if got := r.AddrUsedBy(tt.args.connID); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Region.AddrUsedBy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegion_AvailableAddrs(t *testing.T) {
	type fields struct {
		connFor map[*net.TCPAddr]UsedBy
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			name: "contains addresses",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
				&addr1: Unused(),
				&addr2: InUse(2),
			}},
			want: 1,
		},
		{
			name: "all free",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: Unused(),
				&addr1: Unused(),
				&addr2: Unused(),
			}},
			want: 3,
		},
		{
			name: "all used",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			}},
			want: 0,
		},
		{
			name:   "empty",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{}},
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Region{
				connFor: tt.fields.connFor,
			}
			if got := r.AvailableAddrs(); got != tt.want {
				t.Errorf("Region.AvailableAddrs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegion_GetUnusedIP(t *testing.T) {
	type fields struct {
		connFor map[*net.TCPAddr]UsedBy
	}
	type args struct {
		excluding *net.TCPAddr
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *net.TCPAddr
	}{
		{
			name: "happy test with excluding set",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: Unused(),
				&addr1: Unused(),
				&addr2: InUse(2),
			}},
			args: args{excluding: &addr0},
			want: &addr1,
		},
		{
			name: "happy test with no excluding",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
				&addr1: Unused(),
				&addr2: InUse(2),
			}},
			args: args{excluding: nil},
			want: &addr1,
		},
		{
			name: "sad test with no excluding",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			}},
			args: args{excluding: nil},
			want: nil,
		},
		{
			name: "sad test with excluding",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: Unused(),
				&addr1: InUse(1),
				&addr2: InUse(2),
			}},
			args: args{excluding: &addr0},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Region{
				connFor: tt.fields.connFor,
			}
			if got := r.GetUnusedIP(tt.args.excluding); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Region.GetUnusedIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegion_GiveBack(t *testing.T) {
	type fields struct {
		connFor map[*net.TCPAddr]UsedBy
	}
	type args struct {
		addr *net.TCPAddr
	}
	tests := []struct {
		name           string
		fields         fields
		args           args
		wantOk         bool
		availableAfter int
	}{
		{
			name: "sad test with excluding",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr1: InUse(1),
			}},
			args:           args{addr: &addr1},
			wantOk:         true,
			availableAfter: 1,
		},
		{
			name: "sad test with excluding",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr1: InUse(1),
			}},
			args:           args{addr: &addr2},
			wantOk:         false,
			availableAfter: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Region{
				connFor: tt.fields.connFor,
			}
			if gotOk := r.GiveBack(tt.args.addr); gotOk != tt.wantOk {
				t.Errorf("Region.GiveBack() = %v, want %v", gotOk, tt.wantOk)
			}
			if tt.availableAfter != r.AvailableAddrs() {
				t.Errorf("Region.AvailableAddrs() = %v, want %v", r.AvailableAddrs(), tt.availableAfter)
			}
		})
	}
}

func TestRegion_GetAnyAddress(t *testing.T) {
	type fields struct {
		connFor map[*net.TCPAddr]UsedBy
	}
	tests := []struct {
		name    string
		fields  fields
		wantNil bool
	}{
		{
			name:    "Sad test -- GetAnyAddress should only fail if the region is empty",
			fields:  fields{connFor: map[*net.TCPAddr]UsedBy{}},
			wantNil: true,
		},
		{
			name: "Happy test (all addresses unused)",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: Unused(),
			}},
			wantNil: false,
		},
		{
			name: "Happy test (GetAnyAddress can still return addresses used by proxy conns)",
			fields: fields{connFor: map[*net.TCPAddr]UsedBy{
				&addr0: InUse(2),
			}},
			wantNil: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Region{
				connFor: tt.fields.connFor,
			}
			if got := r.GetAnyAddress(); tt.wantNil != (got == nil) {
				t.Errorf("Region.GetAnyAddress() = %v, but should it return nil? %v", got, tt.wantNil)
			}
		})
	}
}
