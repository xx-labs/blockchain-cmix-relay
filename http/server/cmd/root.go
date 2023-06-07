package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	jww "github.com/spf13/jwalterweatherman"
	"github.com/xx-labs/blockchain-cmix-relay/cmix"
	"gitlab.com/elixxir/client/v4/restlike"
)

// Cmix state config variables are global and don't change
var statePath string

// Password is a mandatory flag
var statePassword string

// Logging flags
var logLevel uint // 0 = info, 1 = debug, >1 = trace
var logPath string
var logPrefix string

// rootCmd represents the base command when called without any sub-commands
var rootCmd = &cobra.Command{
	Use:   "server",
	Short: "Runs a cMix HTTP Proxy server",
	Long:  `Server provides a REST Server that handles client requests over cMix to proxy HTTP requests`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize logging
		initLog()

		// Config
		config := cmix.Config{
			LogPrefix:     logPrefix,
			Cert:          cert,
			NdfUrl:        ndfUrl,
			StatePath:     statePath,
			StatePassword: statePassword,
		}

		// Load REST server
		server := cmix.LoadServer(config)

		// Create HTTP proxy
		proxy := &HttpProxy{}
		// Add endpoint
		server.GetEndpoints().Add("/proxy", restlike.Get, proxy.Callback)

		// Start REST server
		server.Start()

		// Set up channel on which to send signal notifications.
		// We must use a buffered channel or risk missing the signal
		// if we're not ready to receive when the signal is sent.
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

		// Block to prevent the program ending until a signal is received
		<-c

		// Stop REST server
		server.Stop()
	},
}

// Execute adds all child commands to the root command and sets flags
// appropriately.  This is called by main.main(). It only needs to
// happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		jww.ERROR.Printf("[%s] Server exiting with error: %s", logPrefix, err.Error())
		os.Exit(1)
	}
	jww.INFO.Printf("[%s] Server exiting without error...", logPrefix)
}

// init is the initialization function for Cobra which defines commands
// and flags.
func init() {
	// Set flags

	// cMix state
	rootCmd.PersistentFlags().StringVarP(&statePath, "statePath", "s", "state", "Path cMix state directory")
	rootCmd.PersistentFlags().StringVarP(&statePassword, "statePassword", "p", "", "Password for cMix state")
	rootCmd.MarkPersistentFlagRequired("statePassword")

	// Logging
	rootCmd.PersistentFlags().UintVarP(&logLevel, "logLevel", "l", 0, "Level of debugging to print (0 = info, 1 = debug, >1 = trace).")
	rootCmd.PersistentFlags().StringVarP(&logPath, "logFile", "f", "server.log", "Path to log file")
	rootCmd.Flags().StringVarP(&logPrefix, "logPrefix", "", "HTTP", "Logging prefix")
}

// initLog initializes logging thresholds and the log path.
func initLog() {
	// Check the level of logs to display
	if logLevel > 1 {
		// Turn on trace logs
		jww.SetLogThreshold(jww.LevelTrace)
	} else if logLevel == 1 {
		// Turn on debugging logs
		jww.SetLogThreshold(jww.LevelDebug)
	} else {
		// Turn on info logs
		jww.SetLogThreshold(jww.LevelInfo)
	}

	// Create log file, overwrites if existing
	logFile, err := os.Create(logPath)
	if err != nil {
		fmt.Printf("[%s] Could not open log file %s!\n", logPrefix, logPath)
	} else {
		jww.SetLogOutput(logFile)
		jww.SetStdoutOutput(io.Discard)
	}
}
