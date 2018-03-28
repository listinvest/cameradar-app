package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Ullaakut/cameradar"
	curl "github.com/andelf/go-curl"
	"github.com/andlabs/ui"
	"github.com/fatih/color"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type options struct {
	Target      string
	Ports       string
	OutputFile  string
	Routes      string
	Credentials string
	Speed       int
	Timeout     int
	EnableLogs  bool
}

func parseArguments() error {

	viper.BindEnv("target", "CAMERADAR_TARGET")
	viper.BindEnv("ports", "CAMERADAR_PORTS")
	viper.BindEnv("nmap-output", "CAMERADAR_NMAP_OUTPUT_FILE")
	viper.BindEnv("custom-routes", "CAMERADAR_CUSTOM_ROUTES")
	viper.BindEnv("custom-credentials", "CAMERADAR_CUSTOM_CREDENTIALS")
	viper.BindEnv("speed", "CAMERADAR_SPEED")
	viper.BindEnv("timeout", "CAMERADAR_TIMEOUT")
	viper.BindEnv("envlogs", "CAMERADAR_LOGS")

	pflag.StringP("target", "t", "", "The target on which to scan for open RTSP streams - required (ex: 172.16.100.0/24)")
	pflag.StringP("ports", "p", "554,8554", "The ports on which to search for RTSP streams")
	pflag.StringP("nmap-output", "o", "/tmp/cameradar_scan.xml", "The path where nmap will create its XML result file")
	pflag.StringP("custom-routes", "r", "<GOPATH>/src/github.com/Ullaakut/cameradar/dictionaries/routes", "The path on which to load a custom routes dictionary")
	pflag.StringP("custom-credentials", "c", "<GOPATH>/src/github.com/Ullaakut/cameradar/dictionaries/credentials.json", "The path on which to load a custom credentials JSON dictionary")
	pflag.IntP("speed", "s", 4, "The nmap speed preset to use")
	pflag.IntP("timeout", "T", 2000, "The timeout in miliseconds to use for attack attempts")
	pflag.BoolP("log", "l", false, "Enable the logs for nmap's output to stdout")
	pflag.BoolP("help", "h", false, "displays this help message")

	viper.AutomaticEnv()

	pflag.Parse()

	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		return err
	}

	if viper.GetBool("help") {
		pflag.Usage()
		fmt.Println("\nExamples of usage:")
		fmt.Println("\tScanning your home network for RTSP streams:\tcameradar -t 192.168.0.0/24")
		fmt.Println("\tScanning a remote camera on a specific port:\tcameradar -t 172.178.10.14 -p 18554 -s 2")
		fmt.Println("\tScanning an unstable remote network: \t\tcameradar -t 172.178.10.14/24 -s 1 --timeout 10000 -l")
		os.Exit(0)
	}

	return nil
}

func main() {
	var options options

	err := parseArguments()
	if err != nil {
		printErr(err)
	}

	options.Credentials = viper.GetString("custom-credentials")
	options.EnableLogs = viper.GetBool("log") || viper.GetBool("envlogs")
	options.OutputFile = viper.GetString("nmap-output")
	options.Ports = viper.GetString("ports")
	options.Routes = viper.GetString("custom-routes")
	options.Speed = viper.GetInt("speed")
	options.Timeout = viper.GetInt("timeout")
	options.Target = viper.GetString("target")

	err = curl.GlobalInit(curl.GLOBAL_ALL)
	handle := curl.EasyInit()
	if err != nil || handle == nil {
		printErr(errors.New("libcurl initialization failed"))
	}
	c := &cmrdr.Curl{CURL: handle}
	defer curl.GlobalCleanup()

	gopath := os.Getenv("GOPATH")
	options.Credentials = strings.Replace(options.Credentials, "<GOPATH>", gopath, 1)
	options.Routes = strings.Replace(options.Routes, "<GOPATH>", gopath, 1)

	credentials, err := cmrdr.LoadCredentials(options.Credentials)
	if err != nil {
		color.Red("Invalid credentials dictionary: %s", err.Error())
		return
	}

	routes, err := cmrdr.LoadRoutes(options.Routes)
	if err != nil {
		color.Red("Invalid routes dictionary: %s", err.Error())
		return
	}

	err = ui.Main(func() {
		input := ui.NewEntry()
		button := ui.NewButton("Attack")
		results := ui.NewLabel("")
		box := ui.NewVerticalBox()
		box.Append(ui.NewLabel("Targets:"), false)
		box.Append(input, false)
		box.Append(button, false)
		box.Append(results, false)
		window := ui.NewWindow("Cameradar", 200, 100, false)
		window.SetMargined(true)
		window.SetChild(box)
		button.OnClicked(func(*ui.Button) {

			if input.Text() != "" {
				options.Target = input.Text()
			}

			streams, err := cmrdr.Discover(options.Target, options.Ports, options.OutputFile, options.Speed, options.EnableLogs)
			if err != nil && len(streams) > 0 {
				printErr(err)
			}

			// Most cameras will be accessed successfully with these two attacks
			streams, err = cmrdr.AttackRoute(c, streams, routes, time.Duration(options.Timeout)*time.Millisecond, options.EnableLogs)
			streams, err = cmrdr.AttackCredentials(c, streams, credentials, time.Duration(options.Timeout)*time.Millisecond, options.EnableLogs)

			// But some cameras run GST RTSP Server which prioritizes 401 over 404 contrary to most cameras.
			// For these cameras, running another route attack will solve the problem.
			for _, stream := range streams {
				if stream.RouteFound == false || stream.CredentialsFound == false {
					streams, err = cmrdr.AttackRoute(c, streams, routes, time.Duration(options.Timeout)*time.Millisecond, options.EnableLogs)
					break
				}
			}

			prettyPrint(streams, results)
		})
		window.OnClosing(func(*ui.Window) bool {
			ui.Quit()
			return true
		})
		window.Show()
	})
	if err != nil {
		panic(err)
	}
}

func prettyPrint(streams []cmrdr.Stream, results *ui.Label) {
	success := 0

	str := ""
	if len(streams) > 0 {
		for _, stream := range streams {

			if stream.CredentialsFound && stream.RouteFound {
				str = fmt.Sprintf("%s\tDevice RTSP URL:\t%s\n", str, cmrdr.GetCameraRTSPURL(stream))
				success++
			} else {
				str = fmt.Sprintf("%s\tAdmin panel URL:\t%s %s\n", str, cmrdr.GetCameraAdminPanelURL(stream), "You can use this URL to try attacking the camera's admin panel instead.")
			}

			str = fmt.Sprintf("%s\tDevice model:\t\t%s\n\n", str, stream.Device)
			str = fmt.Sprintf("%s\tIP address:\t\t%s\n", str, stream.Address)
			str = fmt.Sprintf("%s\tRTSP port:\t\t%d\n", str, stream.Port)
			if stream.CredentialsFound {
				str = fmt.Sprintf("%s\tUsername:\t\t%s\n", str, stream.Username)
				str = fmt.Sprintf("%s\tPassword:\t\t%s\n", str, stream.Password)
			} else {
				str = fmt.Sprintf("%s\tUsername:\t\t%s\n", str, "not found")
				str = fmt.Sprintf("%s\tPassword:\t\t%s\n", str, "not found")
			}
			if stream.RouteFound {
				str = fmt.Sprintf("%s\tRTSP route:\t\t/%s\n\n\n", str, stream.Route)
			} else {
				str = fmt.Sprintf("%s\tRTSP route:\t\t%s\n\n\n", str, "not found")
			}
		}
		if success > 1 {
			str = fmt.Sprintf("%s Successful attack: %d devices were accessed", str, len(streams))
		} else if success == 1 {
			str = fmt.Sprintf("%s Successful attack: %d device was accessed", str, len(streams))
		} else {
			str = fmt.Sprintf("%s Streams were found but none were accessed. They are most likely configured with secure credentials and routes. You can try adding entries to the dictionary or generating your own in order to attempt a bruteforce attack on the cameras.\n", str)
		}
	} else {
		str = fmt.Sprintf("%s No streams were found. Please make sure that your target is on an accessible network.\n", str)
	}

	results.SetText(str)
}

func printErr(err error) {
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	fmt.Printf("%s %v\n", red("\xE2\x9C\x96"), err)
	os.Exit(1)
}
