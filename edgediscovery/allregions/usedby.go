package allregions

type UsedBy struct {
	ConnID int
	Used   bool
}

func InUse(connID int) UsedBy {
	return UsedBy{ConnID: connID, Used: true}
}

func Unused() UsedBy {
	return UsedBy{}
}
