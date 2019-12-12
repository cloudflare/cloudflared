package protocol

const (
	DBMS_MIN_REVISION_WITH_SERVER_TIMEZONE          = 54058
	DBMS_MIN_REVISION_WITH_QUOTA_KEY_IN_CLIENT_INFO = 54060
)

const (
	ClientHello  = 0
	ClientQuery  = 1
	ClientData   = 2
	ClientCancel = 3
	ClientPing   = 4
)

const (
	CompressEnable  uint64 = 1
	CompressDisable uint64 = 0
)

const (
	StateComplete = 2
)

const (
	ServerHello       = 0
	ServerData        = 1
	ServerException   = 2
	ServerProgress    = 3
	ServerPong        = 4
	ServerEndOfStream = 5
	ServerProfileInfo = 6
	ServerTotals      = 7
	ServerExtremes    = 8
)
