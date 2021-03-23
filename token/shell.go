package token

// OpenBrowser opens the specified URL in the default browser of the user
func OpenBrowser(url string) error {
	return getBrowserCmd(url).Start()
}
