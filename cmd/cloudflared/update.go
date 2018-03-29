package main

import "github.com/equinox-io/equinox"

const appID = "app_idCzgxYerVD"

var publicKey = []byte(`
-----BEGIN ECDSA PUBLIC KEY-----
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAE4OWZocTVZ8Do/L6ScLdkV+9A0IYMHoOf
dsCmJ/QZ6aw0w9qkkwEpne1Lmo6+0pGexZzFZOH6w5amShn+RXt7qkSid9iWlzGq
EKx0BZogHSor9Wy5VztdFaAaVbsJiCbO
-----END ECDSA PUBLIC KEY-----
`)

type ReleaseInfo struct {
	Updated bool
	Version string
	Error   error
}

func checkForUpdates() ReleaseInfo {
	var opts equinox.Options
	if err := opts.SetPublicKeyPEM(publicKey); err != nil {
		return ReleaseInfo{Error: err}
	}

	resp, err := equinox.Check(appID, opts)
	switch {
	case err == equinox.NotAvailableErr:
		return ReleaseInfo{}
	case err != nil:
		return ReleaseInfo{Error: err}
	}

	err = resp.Apply()
	if err != nil {
		return ReleaseInfo{Error: err}
	}

	return ReleaseInfo{Updated: true, Version: resp.ReleaseVersion}
}
