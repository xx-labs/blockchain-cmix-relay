package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	jww "github.com/spf13/jwalterweatherman"
	"github.com/spf13/viper"
	"github.com/xx-labs/blockchain-cmix-relay/cmix"
)

// Cmix state config variables are global and don't change
var statePath string

// Password is a mandatory flag
var statePassword string

// Networks configuration file
// If file is changed supported networks are reloaded
// automatically
var networksCfgFile string

// Logging flags
var logLevel uint // 0 = info, 1 = debug, >1 = trace
var logPath string
var logPrefix string

// Metrics
var metricsPort int

// Network manager is global because it can be reloaded
var manager *Manager

// rootCmd represents the base command when called without any sub-commands
var rootCmd = &cobra.Command{
	Use:   "relay",
	Short: "Runs a blockchain cMix relay server",
	Long:  `Relay provides a REST Server that handles client requests over cMix to query/interact with supported blockchain networks`,
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

		// Initialize networks configuration
		networks := initNetworksConfig()

		// Create network manager
		manager = NewManager(networks, server.GetEndpoints())

		// Start REST server
		server.Start()

		// Setup metrics
		metrics := NewMetricsServer(metricsPort)

		// Start metrics server
		go metrics.Start()

		// Set up channel on which to send signal notifications.
		// We must use a buffered channel or risk missing the signal
		// if we're not ready to receive when the signal is sent.
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

		// Block to prevent the program ending until a signal is received
		<-c

		// Stop REST server
		server.Stop()

		// Stop metrics server
		metrics.Stop()
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

	// Networks configuration file
	rootCmd.Flags().StringVarP(&networksCfgFile, "networks", "n", "networks.json", "Path to networks configuration file")

	// Logging
	rootCmd.PersistentFlags().UintVarP(&logLevel, "logLevel", "l", 0, "Level of debugging to print (0 = info, 1 = debug, >1 = trace).")
	rootCmd.PersistentFlags().StringVarP(&logPath, "logFile", "f", "relay.log", "Path to log file")
	rootCmd.Flags().StringVarP(&logPrefix, "logPrefix", "", "RELAY", "Logging prefix")

	// Metrics
	rootCmd.PersistentFlags().IntVarP(&metricsPort, "metricsPort", "m", 9296, "Port for metrics server")
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

// Don't allow repeated reloads
var reloadDelay = 5 * time.Second
var reloaded = false

// initNetworksConfig reads in the networks config file
func initNetworksConfig() map[string][]NetworkConfig {
	// Panic if no networks configuration file is set
	if networksCfgFile == "" {
		jww.FATAL.Panicf("[%s] No networks config file provided.", logPrefix)
	}

	// Panic if configuration file is not available
	f, err := os.Open(networksCfgFile)
	if err != nil {
		jww.FATAL.Panicf("[%s] Could not open config file: %+v", logPrefix, err)
	}
	err = f.Close()
	if err != nil {
		jww.FATAL.Panicf("[%s] Could not close config file: %+v", logPrefix, err)
	}

	// Read config file using viper
	viper.SetConfigFile(networksCfgFile)
	viper.SetConfigType("json")
	if err = viper.ReadInConfig(); err != nil {
		jww.FATAL.Panicf("[%s] Unable to read networks config file (%s): %s", logPrefix, networksCfgFile, err.Error())
	}
	var networks map[string][]NetworkConfig
	if err = viper.Unmarshal(&networks); err != nil {
		jww.FATAL.Panicf("[%s] Unable to unmarshall networks JSON: %s", logPrefix, err.Error())
	}

	// Setup networks config reloading
	viper.OnConfigChange(func(e fsnotify.Event) {
		if e.Op == fsnotify.Write && !reloaded {
			jww.INFO.Printf("[%s] Reloading networks configuration", logPrefix)
			var newNetworks map[string][]NetworkConfig
			if err = viper.Unmarshal(&newNetworks); err != nil {
				jww.ERROR.Printf("[%s] Unable to unmarshall new networks configuration JSON: %s", logPrefix, err.Error())
			} else {
				jww.INFO.Printf("[%s] Reloading network manager", logPrefix)
				manager.Reload(newNetworks)
				reloaded = true
				// Clear reloaded flag after the delay
				time.AfterFunc(reloadDelay, func() {
					reloaded = false
				})
			}
		}
	})
	viper.WatchConfig()
	return networks
}
