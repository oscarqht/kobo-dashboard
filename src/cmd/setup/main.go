// setup runs only on the user's computer. The Kobo needs no companion server.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	"kobo-dashboard/src/internal/providers"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Setup:", err)
		os.Exit(1)
	}
}
func run() error {
	clientPath := flag.String("client", "", "Google Desktop OAuth client JSON downloaded from Google Cloud")
	output := flag.String("output", "credentials.json", "new private file to copy to the Kobo")
	noBrowser := flag.Bool("no-browser", false, "print authorization URL without opening the browser")
	flag.Parse()
	if *clientPath == "" {
		return errors.New("pass -client /path/to/desktop-client.json; see docs/google-setup.md")
	}
	f, err := os.Open(*clientPath)
	if err != nil {
		return err
	}
	var conf struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&conf)
	f.Close()
	if err != nil || conf.Installed.ClientID == "" {
		return errors.New("expected a Google Desktop app client JSON")
	}
	if _, err := os.Stat(*output); !os.IsNotExist(err) {
		return errors.New("output already exists or cannot be accessed; choose a new path")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	state, err := providers.RandomSecret()
	if err != nil {
		listener.Close()
		return err
	}
	verifier, err := providers.RandomSecret()
	if err != nil {
		listener.Close()
		return err
	}
	redirect := "http://" + listener.Addr().String() + "/callback"
	result := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", callback(state, result))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(listener)
	defer srv.Close()
	auth := providers.AuthorizationURL(conf.Installed.ClientID, redirect, state, verifier)
	fmt.Println("Authorize read-only Google Calendar access in your browser:")
	fmt.Println(auth)
	if !*noBrowser {
		openBrowser(auth)
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 5*time.Minute)
	defer cancel()
	var code string
	select {
	case code = <-result:
	case <-ctx.Done():
		return errors.New("authorization cancelled or timed out")
	}
	if code == "" {
		return errors.New("authorization was denied")
	}
	token, err := providers.Exchange(ctx, nil, providers.TokenURL, url.Values{"code": {code}, "client_id": {conf.Installed.ClientID}, "client_secret": {conf.Installed.ClientSecret}, "redirect_uri": {redirect}, "grant_type": {"authorization_code"}, "code_verifier": {verifier}})
	if err != nil {
		return err
	}
	if token.RefreshToken == "" {
		return errors.New("Google did not return offline authorization; revoke this app's access and try again")
	}
	c := providers.Credentials{ClientID: conf.Installed.ClientID, ClientSecret: conf.Installed.ClientSecret, RefreshToken: token.RefreshToken}
	if err := providers.WriteCredentials(*output, c); err != nil {
		return err
	}
	fmt.Println("Private credentials saved. Copy this file over USB to the dashboard's private data directory as credentials.json. Do not share it.")
	return nil
}

func callback(state string, result chan<- string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		q := r.URL.Query()
		if len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid authorization state", 400)
			return
		}
		code := q.Get("code")
		if q.Get("error") == "" && (len(q["code"]) != 1 || code == "") {
			http.Error(w, "Missing authorization code", 400)
			return
		}
		select {
		case result <- code:
			fmt.Fprintln(w, "Authorization received. Return to the setup helper.")
		default:
			http.Error(w, "Already received", 409)
		}
	}
}
func openBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Run()
}
