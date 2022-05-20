package allregions

import (
	"reflect"
	"testing"
)

func TestAddrSet_AddrUsedBy(t *testing.T) {
	type args struct {
		connID int
	}
	tests := []struct {
		name    string
		addrSet AddrSet
		args    args
		want    *EdgeAddr
	}{
		{
			name: "happy trivial test",
			addrSet: AddrSet{
				&addr0: InUse(0),
			},
			args: args{connID: 0},
			want: &addr0,
		},
		{
			name: "sad trivial test",
			addrSet: AddrSet{
				&addr0: InUse(0),
			},
			args: args{connID: 1},
			want: nil,
		},
		{
			name: "sad test",
			addrSet: AddrSet{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			},
			args: args{connID: 3},
			want: nil,
		},
		{
			name: "happy test",
			addrSet: AddrSet{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			},
			args: args{connID: 1},
			want: &addr1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addrSet.AddrUsedBy(tt.args.connID); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Region.AddrUsedBy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddrSet_AvailableAddrs(t *testing.T) {
	tests := []struct {
		name    string
		addrSet AddrSet
		want    int
	}{
		{
			name: "contains addresses",
			addrSet: AddrSet{
				&addr0: InUse(0),
				&addr1: Unused(),
				&addr2: InUse(2),
			},
			want: 1,
		},
		{
			name: "all free",
			addrSet: AddrSet{
				&addr0: Unused(),
				&addr1: Unused(),
				&addr2: Unused(),
			},
			want: 3,
		},
		{
			name: "all used",
			addrSet: AddrSet{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			},
			want: 0,
		},
		{
			name:    "empty",
			addrSet: AddrSet{},
			want:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addrSet.AvailableAddrs(); got != tt.want {
				t.Errorf("Region.AvailableAddrs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddrSet_GetUnusedIP(t *testing.T) {
	type args struct {
		excluding *EdgeAddr
	}
	tests := []struct {
		name    string
		addrSet AddrSet
		args    args
		want    *EdgeAddr
	}{
		{
			name: "happy test with excluding set",
			addrSet: AddrSet{
				&addr0: Unused(),
				&addr1: Unused(),
				&addr2: InUse(2),
			},
			args: args{excluding: &addr0},
			want: &addr1,
		},
		{
			name: "happy test with no excluding",
			addrSet: AddrSet{
				&addr0: InUse(0),
				&addr1: Unused(),
				&addr2: InUse(2),
			},
			args: args{excluding: nil},
			want: &addr1,
		},
		{
			name: "sad test with no excluding",
			addrSet: AddrSet{
				&addr0: InUse(0),
				&addr1: InUse(1),
				&addr2: InUse(2),
			},
			args: args{excluding: nil},
			want: nil,
		},
		{
			name: "sad test with excluding",
			addrSet: AddrSet{
				&addr0: Unused(),
				&addr1: InUse(1),
				&addr2: InUse(2),
			},
			args: args{excluding: &addr0},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addrSet.GetUnusedIP(tt.args.excluding); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Region.GetUnusedIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddrSet_GiveBack(t *testing.T) {
	type args struct {
		addr *EdgeAddr
	}
	tests := []struct {
		name           string
		addrSet        AddrSet
		args           args
		wantOk         bool
		availableAfter int
	}{
		{
			name: "sad test with excluding",
			addrSet: AddrSet{
				&addr1: InUse(1),
			},
			args:           args{addr: &addr1},
			wantOk:         true,
			availableAfter: 1,
		},
		{
			name: "sad test with excluding",
			addrSet: AddrSet{
				&addr1: InUse(1),
			},
			args:           args{addr: &addr2},
			wantOk:         false,
			availableAfter: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotOk := tt.addrSet.GiveBack(tt.args.addr); gotOk != tt.wantOk {
				t.Errorf("Region.GiveBack() = %v, want %v", gotOk, tt.wantOk)
			}
			if tt.availableAfter != tt.addrSet.AvailableAddrs() {
				t.Errorf("Region.AvailableAddrs() = %v, want %v", tt.addrSet.AvailableAddrs(), tt.availableAfter)
			}
		})
	}
}

func TestAddrSet_GetAnyAddress(t *testing.T) {
	tests := []struct {
		name    string
		addrSet AddrSet
		wantNil bool
	}{
		{
			name:    "Sad test -- GetAnyAddress should only fail if the region is empty",
			addrSet: AddrSet{},
			wantNil: true,
		},
		{
			name: "Happy test (all addresses unused)",
			addrSet: AddrSet{
				&addr0: Unused(),
			},
			wantNil: false,
		},
		{
			name: "Happy test (GetAnyAddress can still return addresses used by proxy conns)",
			addrSet: AddrSet{
				&addr0: InUse(2),
			},
			wantNil: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addrSet.GetAnyAddress(); tt.wantNil != (got == nil) {
				t.Errorf("Region.GetAnyAddress() = %v, but should it return nil? %v", got, tt.wantNil)
			}
		})
	}
}
