package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"mini-proxy/internal/app"
	"mini-proxy/internal/uiauto"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "version":
		fmt.Println("mini-proxy dev")
	case "serve":
		err = serve(os.Args[2:])
	case "install-cert":
		err = app.InstallCert()
	case "uninstall-cert":
		err = app.UninstallCert()
	case "cert-status":
		err = app.CertStatus()
	case "proxy-on":
		err = proxyOn(os.Args[2:])
	case "proxy-restore":
		err = app.RestoreSystemProxy()
	case "uiauto-run":
		err = uiautoRun(os.Args[2:])
	case "uiauto-inspect":
		err = uiautoInspect(os.Args[2:])
	case "uiauto-background-probe":
		err = uiautoBackgroundProbe(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := flags.String("addr", "127.0.0.1:8899", "proxy listen address")
	rulesPath := flags.String("rules", "configs/jd.rules.json", "rules JSON path")
	systemProxy := flags.Bool("system-proxy", false, "enable Windows system proxy while running")
	proxyOverride := flags.String("proxy-override", "localhost;127.0.0.1;<local>", "Windows proxy bypass list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return app.Serve(app.ServeOptions{
		Addr:              *addr,
		RulesPath:         *rulesPath,
		EnableSystemProxy: *systemProxy,
		ProxyOverride:     *proxyOverride,
	})
}

func proxyOn(args []string) error {
	flags := flag.NewFlagSet("proxy-on", flag.ExitOnError)
	addr := flags.String("addr", "127.0.0.1:8899", "proxy server address")
	proxyOverride := flags.String("proxy-override", "localhost;127.0.0.1;<local>", "Windows proxy bypass list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return app.EnableSystemProxy(*addr, *proxyOverride)
}

func uiautoRun(args []string) error {
	flags := flag.NewFlagSet("uiauto-run", flag.ExitOnError)
	configPath := flags.String("config", "configs/example.automation.json", "automation JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return app.RunAutomation(*configPath)
}

func uiautoInspect(args []string) error {
	flags := flag.NewFlagSet("uiauto-inspect", flag.ExitOnError)
	configPath := flags.String("config", "configs/example.automation.json", "automation JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return app.InspectAutomation(*configPath)
}

func uiautoBackgroundProbe(args []string) error {
	flags := flag.NewFlagSet("uiauto-background-probe", flag.ContinueOnError)
	processName := flags.String("process", "WeChatAppEx", "target process name")
	titleContains := flags.String("title-contains", "京东", "target window title substring")
	xRatio := flags.Float64("x-ratio", 0, "horizontal click ratio within the target window")
	yRatio := flags.Float64("y-ratio", 0, "vertical click ratio within the target window")
	candidate := flags.Int("candidate", 0, "ranked target descendant index")
	if err := flags.Parse(args); err != nil {
		return err
	}
	result, err := uiauto.ProbeBackgroundClick(uiauto.BackgroundProbeOptions{
		ProcessName:         *processName,
		WindowTitleContains: *titleContains,
		XRatio:              *xRatio,
		YRatio:              *yRatio,
		CandidateIndex:      *candidate,
	})
	content, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Println(string(content))
	return err
}

func usage() {
	fmt.Fprintln(os.Stderr, `mini-proxy commands:
  version
	serve [-addr 127.0.0.1:8899] [-rules configs/jd.rules.json] [-system-proxy]
  install-cert
  uninstall-cert
  cert-status
  proxy-on [-addr 127.0.0.1:8899]
  proxy-restore
  uiauto-run [-config configs/example.automation.json]
	uiauto-inspect [-config configs/example.automation.json]
	uiauto-background-probe -x-ratio 0.10 -y-ratio 0.108 [-candidate 0]`)
}
