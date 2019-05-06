package raven

type causer interface {
	Cause() error
}

type errWrappedWithExtra struct {
	err       error
	extraInfo map[string]interface{}
}

func (ewx *errWrappedWithExtra) Error() string {
	return ewx.err.Error()
}

func (ewx *errWrappedWithExtra) Cause() error {
	return ewx.err
}

func (ewx *errWrappedWithExtra) ExtraInfo() Extra {
	return ewx.extraInfo
}

// Adds extra data to an error before reporting to Sentry
func WrapWithExtra(err error, extraInfo map[string]interface{}) error {
	return &errWrappedWithExtra{
		err:       err,
		extraInfo: extraInfo,
	}
}

type ErrWithExtra interface {
	Error() string
	Cause() error
	ExtraInfo() Extra
}

// Iteratively fetches all the Extra data added to an error,
// and it's underlying errors. Extra data defined first is
// respected, and is not overridden when extracting.
func extractExtra(err error) Extra {
	extra := Extra{}

	currentErr := err
	for currentErr != nil {
		if errWithExtra, ok := currentErr.(ErrWithExtra); ok {
			for k, v := range errWithExtra.ExtraInfo() {
				extra[k] = v
			}
		}

		if errWithCause, ok := currentErr.(causer); ok {
			currentErr = errWithCause.Cause()
		} else {
			currentErr = nil
		}
	}

	return extra
}
