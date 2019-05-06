package cli

import (
	"fmt"
	"io/ioutil"
	"sort"
	"strings"
)

// Command is a subcommand for a cli.App.
type Command struct {
	// The name of the command
	Name string
	// A list of aliases for the command
	Aliases []string
	// A short description of the usage of this command
	Usage string
	// Custom text to show on USAGE section of help
	UsageText string
	// A longer explanation of how the command works
	Description string
	// A short description of the arguments of this command
	ArgsUsage string
	// The category the command is part of
	Category string
	// The function to call when checking for shell command completions
	ShellComplete ShellCompleteFunc
	// An action to execute before any sub-subcommands are run, but after the context is ready
	// If a non-nil error is returned, no sub-subcommands are run
	Before BeforeFunc
	// An action to execute after any subcommands are run, but after the subcommand has finished
	// It is run even if Action() panics
	After AfterFunc
	// The function to call when this command is invoked
	Action ActionFunc
	// Execute this function if a usage error occurs.
	OnUsageError OnUsageErrorFunc
	// List of child commands
	Subcommands []*Command
	// List of flags to parse
	Flags []Flag
	// Treat all flags as normal arguments if true
	SkipFlagParsing bool
	// Boolean to hide built-in help command
	HideHelp bool
	// Boolean to hide this command from help or completion
	Hidden bool

	// Full name of command for help, defaults to full command name, including parent commands.
	HelpName        string
	commandNamePath []string

	// CustomHelpTemplate the text template for the command help topic.
	// cli.go uses text/template to render templates. You can
	// render custom help text by setting this variable.
	CustomHelpTemplate string
}

type CommandsByName []*Command

func (c CommandsByName) Len() int {
	return len(c)
}

func (c CommandsByName) Less(i, j int) bool {
	return c[i].Name < c[j].Name
}

func (c CommandsByName) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

// FullName returns the full name of the command.
// For subcommands this ensures that parent commands are part of the command path
func (c *Command) FullName() string {
	if c.commandNamePath == nil {
		return c.Name
	}
	return strings.Join(c.commandNamePath, " ")
}

// Run invokes the command given the context, parses ctx.Args() to generate command-specific flags
func (c *Command) Run(ctx *Context) (err error) {
	if len(c.Subcommands) > 0 {
		return c.startApp(ctx)
	}

	if !c.HideHelp && HelpFlag != nil {
		// append help to flags
		c.appendFlag(HelpFlag)
	}

	if ctx.App.EnableShellCompletion {
		c.appendFlag(GenerateCompletionFlag)
	}

	set, err := flagSet(c.Name, c.Flags)
	if err != nil {
		return err
	}
	set.SetOutput(ioutil.Discard)

	if c.SkipFlagParsing {
		err = set.Parse(append([]string{"--"}, ctx.Args().Tail()...))
	} else {
		err = set.Parse(ctx.Args().Tail())
	}

	nerr := normalizeFlags(c.Flags, set)
	if nerr != nil {
		fmt.Fprintln(ctx.App.Writer, nerr)
		fmt.Fprintln(ctx.App.Writer)
		ShowCommandHelp(ctx, c.Name)
		return nerr
	}

	context := NewContext(ctx.App, set, ctx)
	context.Command = c
	if checkCommandCompletions(context, c.Name) {
		return nil
	}

	if err != nil {
		if c.OnUsageError != nil {
			err = c.OnUsageError(context, err, false)
			HandleExitCoder(err)
			return err
		}
		fmt.Fprintln(context.App.Writer, "Incorrect Usage:", err.Error())
		fmt.Fprintln(context.App.Writer)
		ShowCommandHelp(context, c.Name)
		return err
	}

	if checkCommandHelp(context, c.Name) {
		return nil
	}

	if c.After != nil {
		defer func() {
			afterErr := c.After(context)
			if afterErr != nil {
				HandleExitCoder(err)
				if err != nil {
					err = newMultiError(err, afterErr)
				} else {
					err = afterErr
				}
			}
		}()
	}

	if c.Before != nil {
		err = c.Before(context)
		if err != nil {
			ShowCommandHelp(context, c.Name)
			HandleExitCoder(err)
			return err
		}
	}

	if c.Action == nil {
		c.Action = helpSubcommand.Action
	}

	context.Command = c
	err = c.Action(context)

	if err != nil {
		HandleExitCoder(err)
	}
	return err
}

// Names returns the names including short names and aliases.
func (c *Command) Names() []string {
	return append([]string{c.Name}, c.Aliases...)
}

// HasName returns true if Command.Name matches given name
func (c *Command) HasName(name string) bool {
	for _, n := range c.Names() {
		if n == name {
			return true
		}
	}
	return false
}

func (c *Command) startApp(ctx *Context) error {
	app := &App{
		Metadata: ctx.App.Metadata,
		Name:     fmt.Sprintf("%s %s", ctx.App.Name, c.Name),
	}

	if c.HelpName == "" {
		app.HelpName = c.HelpName
	} else {
		app.HelpName = app.Name
	}

	app.Usage = c.Usage
	app.Description = c.Description
	app.ArgsUsage = c.ArgsUsage

	// set CommandNotFound
	app.CommandNotFound = ctx.App.CommandNotFound
	app.CustomAppHelpTemplate = c.CustomHelpTemplate

	// set the flags and commands
	app.Commands = c.Subcommands
	app.Flags = c.Flags
	app.HideHelp = c.HideHelp

	app.Version = ctx.App.Version
	app.HideVersion = ctx.App.HideVersion
	app.Compiled = ctx.App.Compiled
	app.Writer = ctx.App.Writer
	app.ErrWriter = ctx.App.ErrWriter

	app.Categories = newCommandCategories()
	for _, command := range c.Subcommands {
		app.Categories.AddCommand(command.Category, command)
	}

	sort.Sort(app.Categories.(*commandCategories))

	// bash completion
	app.EnableShellCompletion = ctx.App.EnableShellCompletion
	if c.ShellComplete != nil {
		app.ShellComplete = c.ShellComplete
	}

	// set the actions
	app.Before = c.Before
	app.After = c.After
	if c.Action != nil {
		app.Action = c.Action
	} else {
		app.Action = helpSubcommand.Action
	}
	app.OnUsageError = c.OnUsageError

	for index, cc := range app.Commands {
		app.Commands[index].commandNamePath = []string{c.Name, cc.Name}
	}

	return app.RunAsSubcommand(ctx)
}

// VisibleFlags returns a slice of the Flags with Hidden=false
func (c *Command) VisibleFlags() []Flag {
	return visibleFlags(c.Flags)
}

func (c *Command) appendFlag(fl Flag) {
	if !hasFlag(c.Flags, fl) {
		c.Flags = append(c.Flags, fl)
	}
}

func hasCommand(commands []*Command, command *Command) bool {
	for _, existing := range commands {
		if command == existing {
			return true
		}
	}

	return false
}
