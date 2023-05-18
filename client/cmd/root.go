package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	jww "github.com/spf13/jwalterweatherman"
	"github.com/xx-labs/blockchain-cmix-relay/client/api"
)

// Cmix state config variables are global and don't change
var statePath string

// Password is a mandatory flag
var statePassword string

var ndfUrl string
var cert string

// Server contact file
var contactFiles []string

// Logging flags
var logLevel uint // 0 = info, 1 = debug, >1 = trace
var logPath string
var logPrefix string

// Request retries
var retries int

// Local HTTP proxy server port
var port int

// rootCmd represents the base command when called without any sub-commands
var rootCmd = &cobra.Command{
	Use:   "client",
	Short: "Runs a blockchain cMix relay client",
	Long:  `Client provides an HTTP server that proxies JSON-RPC requests over cMix to query/interact with supported blockchain networks`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize logging
		initLog()

		// Relay servers
		serverContacts := make([]api.ServerInfo, len(contactFiles))
		for i, contactFile := range contactFiles {
			serverContacts[i] = api.ServerInfo{
				ContactFile: contactFile,
				Name:        fmt.Sprintf("relay-%d", i),
			}
		}

		// Create API
		config := api.Config{
			LogPrefix:      logPrefix,
			Retries:        retries,
			Cert:           cert,
			NdfUrl:         ndfUrl,
			StatePath:      statePath,
			StatePassword:  statePassword,
			ServerContacts: serverContacts,
		}
		apiInstance := api.NewApi(config)

		// Connect API
		apiInstance.Connect()

		// Create HTTP proxy server
		server := api.NewHttpProxy(apiInstance, port, logPrefix)

		// Print supported networks
		networks := apiInstance.Networks()
		jww.INFO.Printf("[%s] Supported networks", logPrefix)
		for _, net := range networks {
			jww.INFO.Printf("[%s] http://localhost:%d%s", logPrefix, port, net)
		}

		// Start server
		go server.Start()

		// Handle shutdown
		done := make(chan os.Signal, 1)
		signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

		// Block until signal
		<-done

		// Stop HTTP server
		server.Stop()

		// Disconnect API
		apiInstance.Disconnect()

		time.Sleep(2 * time.Second)
	},
}

// Execute adds all child commands to the root command and sets flags
// appropriately.  This is called by main.main(). It only needs to
// happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		jww.ERROR.Printf("[%s] Client exiting with error: %s", logPrefix, err.Error())
		os.Exit(1)
	}
	jww.INFO.Printf("[%s] Client exiting without error...", logPrefix)
}

// init is the initialization function for Cobra which defines commands
// and flags.
func init() {
	// Set flags

	// cMix state
	rootCmd.Flags().StringVarP(&statePath, "statePath", "s", "state", "Path cMix state directory")
	rootCmd.Flags().StringVarP(&statePassword, "statePassword", "p", "", "Password for cMix state")
	rootCmd.MarkFlagRequired("statePassword")
	rootCmd.Flags().StringVarP(&ndfUrl, "ndf", "d",
		"https://elixxir-bins.s3.us-west-1.amazonaws.com/ndf/mainnet.json",
		"URL used to download NDF file on initialization",
	)
	rootCmd.Flags().StringVarP(&cert, "cert", "r",
		"mainnet.crt",
		"Path to certificate file used to verify NDF download",
	)

	// Contact file
	rootCmd.Flags().StringArrayVarP(&contactFiles, "contactFiles", "c", []string{"relay.xxc"}, "List of paths to files containing the REST server contact info")
	// Retries
	rootCmd.Flags().IntVarP(&retries, "retries", "n", 3, "How many times to retry sending request over cMix")
	// Port
	rootCmd.Flags().IntVarP(&port, "port", "t", 9296, "Port to listen on for local HTTP proxy server")

	// Logging
	rootCmd.Flags().UintVarP(&logLevel, "logLevel", "l", 0, "Level of debugging to print (0 = info, 1 = debug, >1 = trace).")
	rootCmd.Flags().StringVarP(&logPath, "logFile", "f", "client.log", "Path to log file")
	rootCmd.Flags().StringVarP(&logPrefix, "logPrefix", "", "RELAY", "Logging prefix")
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
		fmt.Printf("[%v] Could not open log file %s!\n", logPrefix, logPath)
	} else {
		jww.SetLogOutput(logFile)
		jww.SetStdoutOutput(io.Discard)
	}
}
