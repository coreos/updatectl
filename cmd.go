package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/tabwriter"
	"strconv"

	"github.com/coreos/updatectl/auth"
	"github.com/coreos/updatectl/client/update/v1"
	"github.com/coreos/updatectl/version"
)

const (
	OK = iota
	// Error Codes
	ERROR_API
	ERROR_USAGE
	ERROR_NO_COMMAND

	cliName        = "updatectl"
	cliDescription = "updatectl is a command line driven interface to the roller."
)

type StringFlag struct {
	value    *string
	required bool
}

func (f *StringFlag) Set(value string) error {
	f.value = &value
	return nil
}

func (f *StringFlag) Get() *string {
	return f.value
}

func (f *StringFlag) String() string {
	if f.value != nil {
		return *f.value
	}
	return ""
}

type BoolFlag struct {
	value    *bool
}

func (f *BoolFlag) Set(value string) error {
	falseVals := []string{"0", "f", "false", "FALSE", "False"}
	truthVals := []string{"1", "t", "true", "TRUE", "True"}
	for _, val := range falseVals {
		if val == value {
			*f.value = false
		}
	}
	for _, val := range truthVals {
		if val == value {
			f.value = new(bool)
			*f.value = true
		}
	}
	if f.value == nil {
		return fmt.Errorf("value must be one of %v, %v", truthVals, falseVals)
	}
	return nil
}

func (f *BoolFlag) Get() *bool {
	return f.value
}

func (f *BoolFlag) String() string {
	if f.value != nil {
		return strconv.FormatBool(*f.value)
	}
	return ""
}

type Command struct {
	Name        string       // Name of the Command and the string to use to invoke it
	Summary     string       // One-sentence summary of what the Command does
	Usage       string       // Usage options/arguments
	Description string       // Detailed description of command
	Flags       flag.FlagSet // Set of flags associated with this command
	Run         handlerFunc  // Run a command with the given arguments
	Subcommands []*Command   // Subcommands for this command.
}

var (
	out           *tabwriter.Writer
	globalFlagSet *flag.FlagSet
	commands      []*Command

	globalFlags struct {
		Server  string
		User    string
		Key     string
		Debug   bool
		Version bool
		Help    bool
	}
)

func init() {
	out = new(tabwriter.Writer)
	out.Init(os.Stdout, 0, 8, 1, '\t', 0)

	server := "http://localhost:8000" // default server
	if serverEnv := os.Getenv("UPDATECTL_SERVER"); serverEnv != "" {
		server = serverEnv
	}

	globalFlagSet = flag.NewFlagSet(cliName, flag.ExitOnError)
	globalFlagSet.StringVar(&globalFlags.Server, "server", server, "Update server to connect to")
	globalFlagSet.BoolVar(&globalFlags.Debug, "debug", false, "Output debugging info to stderr")
	globalFlagSet.BoolVar(&globalFlags.Version, "version", false, "Print version information and exit.")
	globalFlagSet.BoolVar(&globalFlags.Help, "help", false, "Print usage information and exit.")
	globalFlagSet.StringVar(&globalFlags.User, "user", os.Getenv("UPDATECTL_USER"), "API Username")
	globalFlagSet.StringVar(&globalFlags.Key, "key", os.Getenv("UPDATECTL_KEY"), "API Key")

	commands = []*Command{
		// admin.go
		cmdAdminUser,
		// app.go
		cmdApp,
		// channel.go
		cmdChannel,
		// database.go
		cmdDatabase,
		// group.go
		cmdGroup,
		// help.go
		cmdHelp,
		// instance.go
		cmdInstance,
		// pkg.go
		cmdPackage,
		// watch.go
		cmdWatch,
	}
}

type handlerFunc func([]string, *update.Service, *tabwriter.Writer) int

func getHawkClient(user string, key string) *http.Client {
	return &http.Client{Transport: &auth.HawkRoundTripper{user, key}}
}

func handle(fn handlerFunc) func(f *flag.FlagSet) int {
	return func(f *flag.FlagSet) (exit int) {
		user := globalFlags.User
		key := globalFlags.Key
		client := getHawkClient(user, key)

		service, err := update.New(client)
		if err != nil {
			log.Fatal(err)
		}

		service.BasePath = globalFlags.Server + "/_ah/api/update/v1/"
		exit = fn(f.Args(), service, out)
		return
	}
}

func printVersion(out *tabwriter.Writer) {
	fmt.Fprintf(out, "%s version %s\n", cliName, version.Version)
	out.Flush()
}

func getAllFlags() (flags []*flag.Flag) {
	return getFlags(globalFlagSet)
}

func getFlags(flagset *flag.FlagSet) (flags []*flag.Flag) {
	flags = make([]*flag.Flag, 0)
	flagset.VisitAll(func(f *flag.Flag) {
		flags = append(flags, f)
	})
	return
}

// determine which Command should be run
func findCommand(search string, args []string, commands []*Command) (cmd *Command, name string) {
	if len(args) < 1 {
		return
	}
	if search == "" {
		search = args[0]
	} else {
		search = fmt.Sprintf("%s %s", search, args[0])
	}
	name = search
	for _, c := range commands {
		if c.Name == search {
			cmd = c
			if errHelp := c.Flags.Parse(args[1:]); errHelp != nil {
				printCommandUsage(cmd)
				os.Exit(ERROR_USAGE)
			}
			if len(cmd.Subcommands) != 0 {
				subArgs := cmd.Flags.Args()
				var subCmd *Command
				subCmd, name = findCommand(search, subArgs, cmd.Subcommands)
				if subCmd != nil {
					cmd = subCmd
				}
			}
			break
		}
	}
	return
}

func main() {
	globalFlagSet.Parse(os.Args[1:])
	var args = globalFlagSet.Args()

	if globalFlags.Version {
		printVersion(out)
		os.Exit(OK)
	}

	if globalFlags.Help {
		printGlobalUsage()
		os.Exit(OK)
	}

	// no command specified - trigger help
	if len(args) < 1 {
		args = append(args, "help")
	}

	cmd, name := findCommand("", args, commands)

	if cmd == nil {
		fmt.Printf("%v: unknown subcommand: %q\n", cliName, name)
		fmt.Printf("Run '%v help' for usage.\n", cliName)
		os.Exit(ERROR_NO_COMMAND)
	}

	if cmd.Run == nil {
		printCommandUsage(cmd)
		os.Exit(ERROR_USAGE)
	} else {
		exit := handle(cmd.Run)(&cmd.Flags)
		if exit == ERROR_USAGE {
			printCommandUsage(cmd)
		}
		os.Exit(exit)
	}
}
