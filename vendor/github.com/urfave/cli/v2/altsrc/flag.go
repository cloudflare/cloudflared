package altsrc

import (
	"fmt"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/urfave/cli/v2"
)

// FlagInputSourceExtension is an extension interface of cli.Flag that
// allows a value to be set on the existing parsed flags.
type FlagInputSourceExtension interface {
	cli.Flag
	ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error
}

// ApplyInputSourceValues iterates over all provided flags and
// executes ApplyInputSourceValue on flags implementing the
// FlagInputSourceExtension interface to initialize these flags
// to an alternate input source.
func ApplyInputSourceValues(context *cli.Context, inputSourceContext InputSourceContext, flags []cli.Flag) error {
	for _, f := range flags {
		if err := applyFlagValue(f, context, inputSourceContext); err != nil {
			return err
		}
	}

	return nil
}

// ApplyInputSource sets flags from `inputSource` across cli.Context hierarchy.
// If a flag is defined at multiple levels, this method will try to update only the most specific context
// that uses the flag. If the most specific version of the flag doesn't support loading values from an input source
// the method will not check definitions from less-specific contexts.
func ApplyInputSource(c *cli.Context, inputSource InputSourceContext) error {
	visited := make(map[string]bool)
	for _, context := range c.Lineage() {
		if context.Command == nil {
			// we've reached the placeholder root context not associated with the app
			break
		}
		flags := context.Command.Flags
		if context.Command.Name == "" {
			// commands that define child subcommands are executed as if they were an app
			flags = context.App.Flags
		}
		applyingFlags:
		for _, f := range flags {
			for _, name := range f.Names() {
				if visited[name] {
					continue applyingFlags
				}
				visited[name] = true
			}
			if err := applyFlagValue(f, context, inputSource); err != nil {
				return err
			}
		}
	}

	return nil
}

func applyFlagValue(flag cli.Flag, context *cli.Context, inputSource InputSourceContext) error {
	inputSourceExtendedFlag, isType := flag.(FlagInputSourceExtension)
	if isType {
		err := inputSourceExtendedFlag.ApplyInputSourceValue(context, inputSource)
		if err != nil {
			return err
		}
	}
	return nil
}

// InitInputSource is used to to setup an InputSourceContext on a cli.Command Before method. It will create a new
// input source based on the func provided. If there is no error it will then apply the new input source to any flags
// that are supported by the input source
func InitInputSource(flags []cli.Flag, createInputSource func() (InputSourceContext, error)) cli.BeforeFunc {
	return func(context *cli.Context) error {
		inputSource, err := createInputSource()
		if err != nil {
			return fmt.Errorf("Unable to create input source: inner error: \n'%v'", err.Error())
		}

		return ApplyInputSourceValues(context, inputSource, flags)
	}
}

// InitInputSourceWithContext is used to to setup an InputSourceContext on a cli.Command Before method. It will create a new
// input source based on the func provided with potentially using existing cli.Context values to initialize itself. If there is
// no error it will then apply the new input source to any flags that are supported by the input source
func InitInputSourceWithContext(flags []cli.Flag, createInputSource func(context *cli.Context) (InputSourceContext, error)) cli.BeforeFunc {
	return func(context *cli.Context) error {
		inputSource, err := createInputSource(context)
		if err != nil {
			return fmt.Errorf("Unable to create input source with context: inner error: \n'%v'", err.Error())
		}

		return ApplyInputSourceValues(context, inputSource, flags)
	}
}

// ApplyInputSourceValue applies a generic value to the flagSet if required
func (f *GenericFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !context.IsSet(f.Name) && !isEnvVarSet(f.EnvVars) {
			value, err := isc.Generic(f.GenericFlag.Name)
			if err != nil {
				return err
			}
			if value != nil {
				for _, name := range f.Names() {
					_ = context.Set(name, value.String())
				}
			}
		}
	}

	return nil
}

// ApplyInputSourceValue applies a StringSlice value to the flagSet if required
func (f *StringSliceFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !context.IsSet(f.Name) && !isEnvVarSet(f.EnvVars) {
			value, err := isc.StringSlice(f.StringSliceFlag.Name)
			if err != nil {
				return err
			}
			if value != nil {
				var sliceValue cli.StringSlice = *(cli.NewStringSlice(value...))
				for _, name := range f.Names() {
					underlyingFlag := f.set.Lookup(name)
					if underlyingFlag != nil {
						context.Set(name, sliceValue.Serialize())
						underlyingFlag.Value = &sliceValue
					}
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a IntSlice value if required
func (f *IntSliceFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !context.IsSet(f.Name) && !isEnvVarSet(f.EnvVars) {
			value, err := isc.IntSlice(f.IntSliceFlag.Name)
			if err != nil {
				return err
			}
			if value != nil {
				var sliceValue cli.IntSlice = *(cli.NewIntSlice(value...))
				for _, name := range f.Names() {
					underlyingFlag := f.set.Lookup(name)
					if underlyingFlag != nil {
						context.Set(name, sliceValue.Serialize())
						underlyingFlag.Value = &sliceValue
					}
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a Bool value to the flagSet if required
func (f *BoolFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !context.IsSet(f.Name) && !isEnvVarSet(f.EnvVars) {
			value, err := isc.Bool(f.BoolFlag.Name)
			if err != nil {
				return err
			}
			if value {
				for _, name := range f.Names() {
					_ = context.Set(name, strconv.FormatBool(value))
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a String value to the flagSet if required
func (f *StringFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !(context.IsSet(f.Name) || isEnvVarSet(f.EnvVars)) {
			value, err := isc.String(f.StringFlag.Name)
			if err != nil {
				return err
			}
			if value != "" {
				for _, name := range f.Names() {
					_ = context.Set(name, value)
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a Path value to the flagSet if required
func (f *PathFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !(context.IsSet(f.Name) || isEnvVarSet(f.EnvVars)) {
			value, err := isc.String(f.PathFlag.Name)
			if err != nil {
				return err
			}
			if value != "" {
				for _, name := range f.Names() {

					if !filepath.IsAbs(value) && isc.Source() != "" {
						basePathAbs, err := filepath.Abs(isc.Source())
						if err != nil {
							return err
						}

						value = filepath.Join(filepath.Dir(basePathAbs), value)
					}

					_ = context.Set(name, value)
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a int value to the flagSet if required
func (f *IntFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !(context.IsSet(f.Name) || isEnvVarSet(f.EnvVars)) {
			value, err := isc.Int(f.IntFlag.Name)
			if err != nil {
				return err
			}
			if value > 0 {
				for _, name := range f.Names() {
					_ = context.Set(name, strconv.FormatInt(int64(value), 10))
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a Duration value to the flagSet if required
func (f *DurationFlag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !(context.IsSet(f.Name) || isEnvVarSet(f.EnvVars)) {
			value, err := isc.Duration(f.DurationFlag.Name)
			if err != nil {
				return err
			}
			if value > 0 {
				for _, name := range f.Names() {
					_ = context.Set(name, value.String())
				}
			}
		}
	}
	return nil
}

// ApplyInputSourceValue applies a Float64 value to the flagSet if required
func (f *Float64Flag) ApplyInputSourceValue(context *cli.Context, isc InputSourceContext) error {
	if context != nil {
		if !(context.IsSet(f.Name) || isEnvVarSet(f.EnvVars)) {
			value, err := isc.Float64(f.Float64Flag.Name)
			if err != nil {
				return err
			}
			if value > 0 {
				floatStr := float64ToString(value)
				for _, name := range f.Names() {
					_ = context.Set(name, floatStr)
				}
			}
		}
	}
	return nil
}

func isEnvVarSet(envVars []string) bool {
	for _, envVar := range envVars {
		if _, ok := syscall.Getenv(envVar); ok {
			// TODO: Can't use this for bools as
			// set means that it was true or false based on
			// Bool flag type, should work for other types
			return true
		}
	}

	return false
}

func float64ToString(f float64) string {
	return fmt.Sprintf("%v", f)
}
