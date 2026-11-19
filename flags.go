package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	oci             string
	port            string
	isAllowedOrigin func(string) bool
)

func init() {
	flag.StringVar(&oci, "oci", "podman", "Define used cli command for OCI provider")
	flag.StringVar(&port, "port", ":8080", "Define used http port")
	flag.StringVar(&port, "p", ":8080", "Shorthand for --port")
	pattern := flag.String("origin", "*", "Regexp pattern for origin validation of incoming requets")

	flag.Parse()

	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	if pass, _ := regexp.MatchString(`^:[0-9]{2,5}$`, port); !pass {
		fmt.Fprint(os.Stderr, "Invalid given port '", port, "' ")
		os.Exit(1)
	}

	if _, err := sh(oci, "--version"); err != nil {
		fmt.Fprint(os.Stderr, "Invalid OCI provider '", oci, "' ", err)
		os.Exit(1)
	}

	if *pattern == "*" {
		isAllowedOrigin = nil
	} else if rgx, err := regexp.Compile(*pattern); err == nil {
		isAllowedOrigin = rgx.MatchString
	} else {
		fmt.Fprint(os.Stderr, "Invalid allowed origin regex pattern '", *pattern, "' ", err)
		os.Exit(1)
	}
}
