package credentials

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/cfapi"
)

const (
	logFieldOriginCertPath = "originCertPath"
)

type User struct {
	cert     *OriginCert
	certPath string
}

func (c User) AccountID() string {
	return c.cert.AccountID
}

func (c User) ZoneID() string {
	return c.cert.ZoneID
}

func (c User) APIToken() string {
	return c.cert.APIToken
}

func (c User) CertPath() string {
	return c.certPath
}

// Client uses the user credentials to create a Cloudflare API client
func (c *User) Client(apiURL string, userAgent string, log *zerolog.Logger) (cfapi.Client, error) {
	if apiURL == "" {
		return nil, errors.New("An api-url was not provided for the Cloudflare API client")
	}
	client, err := cfapi.NewRESTClient(
		apiURL,
		c.cert.AccountID,
		c.cert.ZoneID,
		c.cert.APIToken,
		userAgent,
		log,
	)

	if err != nil {
		return nil, err
	}
	return client, nil
}

// Read will load and read the origin cert.pem to load the user credentials
func Read(originCertPath string, log *zerolog.Logger) (*User, error) {
	originCertLog := log.With().
		Str(logFieldOriginCertPath, originCertPath).
		Logger()

	originCertPath, err := FindOriginCert(originCertPath, &originCertLog)
	if err != nil {
		return nil, errors.Wrap(err, "Error locating origin cert")
	}
	blocks, err := readOriginCert(originCertPath)
	if err != nil {
		return nil, errors.Wrapf(err, "Can't read origin cert from %s", originCertPath)
	}

	cert, err := decodeOriginCert(blocks)
	if err != nil {
		return nil, errors.Wrap(err, "Error decoding origin cert")
	}

	if cert.AccountID == "" {
		return nil, errors.Errorf(`Origin certificate needs to be refreshed before creating new tunnels.\nDelete %s and run "cloudflared login" to obtain a new cert.`, originCertPath)
	}

	return &User{
		cert:     cert,
		certPath: originCertPath,
	}, nil
}
