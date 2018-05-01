/*
Package equinox allows applications to remotely update themselves with the equinox.io service.

Minimal Working Example

	import "github.com/equinox-io/equinox"

	const appID = "<YOUR EQUINOX APP ID>"

	var publicKey = []byte(`
	-----BEGIN PUBLIC KEY-----
	MFYwEAYHKoZIzj0CAQYFK4EEAAoDQgAEtrVmBxQvheRArXjg2vG1xIprWGuCyESx
	MMY8pjmjepSy2kuz+nl9aFLqmr+rDNdYvEBqQaZrYMc6k29gjvoQnQ==
	-----END PUBLIC KEY-----
	`)

	func update(channel string) error {
		opts := equinox.Options{Channel: channel}
		if err := opts.SetPublicKeyPEM(publicKey); err != nil {
			return err
		}

		// check for the update
		resp, err := equinox.Check(appID, opts)
		switch {
		case err == equinox.NotAvailableErr:
			fmt.Println("No update available, already at the latest version!")
			return nil
		case err != nil:
			return err
		}

		// fetch the update and apply it
		err = resp.Apply()
		if err != nil {
			return err
		}

		fmt.Printf("Updated to new version: %s!\n", resp.ReleaseVersion)
		return nil
	}


Update To Specific Version

When you specify a channel in the update options, equinox will try to update the application
to the latest release of your application published to that channel. Instead, you may wish to
update the application to a specific (possibly older) version. You can do this by explicitly setting
Version in the Options struct:

		opts := equinox.Options{Version: "0.1.2"}

Prompt For Update

You may wish to ask the user for approval before updating to a new version. This is as simple
as calling the Check function and only calling Apply on the returned result if the user approves.
Example:

	// check for the update
	resp, err := equinox.Check(appID, opts)
	switch {
	case err == equinox.NotAvailableErr:
		fmt.Println("No update available, already at the latest version!")
		return nil
	case err != nil:
		return err
	}

	fmt.Println("New version available!")
	fmt.Println("Version:", resp.ReleaseVersion)
	fmt.Println("Name:", resp.ReleaseTitle)
	fmt.Println("Details:", resp.ReleaseDescription)

	ok := prompt("Would you like to update?")

	if !ok {
		return
	}

	err = resp.Apply()
	// ...

Generating Keys

All equinox releases must be signed with a private ECDSA key, and all updates verified with the
public key portion. To do that, you'll need to generate a key pair. The equinox release tool can
generate an ecdsa key pair for you easily:

	equinox genkey

*/
package equinox
