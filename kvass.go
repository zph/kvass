package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	kvass "github.com/maxmunzel/kvass/src"
	"github.com/teris-io/cli"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

func getPersistance(options map[string]string) *kvass.SqlitePersistance {
	dbpath, contains := options["db"]
	if !contains {

		defaultFilename := ".kvassdb.sqlite"
		home, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		dbpath = path.Join(home, defaultFilename)
	}

	p, err := kvass.NewSqlitePersistance(dbpath)
	if err != nil {
		panic(err)
	}
	return p
}
func main() {
	logger := log.New(os.Stderr, "", log.Llongfile|log.LstdFlags)
	get := cli.NewCommand("get", "get a value").
		WithArg(cli.NewArg("key", "the key to get")).
		WithAction(func(args []string, options map[string]string) int {
			key := args[0]
			p := getPersistance(options)
			defer p.Close()

			err := p.GetRemoteUpdates()
			if err != nil {
				logger.Println("Couldn't get updates from server. ", err)
			}
			val, err := p.GetValue(key)
			if err != nil {
				panic(err)
			}

			_, err = io.Copy(os.Stdout, bytes.NewBuffer(val))
			if err != nil {
				panic(err)
			}
			return 0
		})

	set := cli.NewCommand("set", "set a value").
		WithArg(cli.NewArg("key", "the key to set")).
		WithArg(cli.NewArg("value", "the value to set (ommit for stdin)").AsOptional()).
		WithAction(func(args []string, options map[string]string) int {
			key := args[0]

			p := getPersistance(options)
			defer p.Close()

			var err error
			var val kvass.ValueType

			if len(args) < 2 {
				valBytes, err := ioutil.ReadAll(os.Stdin)
				val = valBytes
				if err != nil {
					panic(err)
				}

			} else {
				val = []byte(args[1] + "\n")
			}

			err = kvass.Set(p, key, []byte(val))
			if err != nil {
				panic(err)
			}

			// push changes to remote

			host := p.State.RemoteHostname
			if host == "" {
				return 0
			}
			updates, err := p.GetUpdates(0)
			if err != nil {
				panic(err)
			}
			payload, err := json.Marshal(updates)
			if err != nil {
				panic(err)
			}
			payload, err = p.Encrypt(payload)
			if err != nil {
				panic(err)
			}

			resp, err := http.DefaultClient.Post("http://"+host+"/push", "application/json", bytes.NewReader(payload))
			if err != nil || resp.StatusCode != 200 {
				logger.Println("Error posting update to server: ", err)
				return 1
			}
			return 0
		})

	serve := cli.NewCommand("serve", "start in server mode").
		WithOption(cli.NewOption("bind", "bind address (default: localhost:8000)")).
		WithAction(func(args []string, options map[string]string) int {
			bind, contains := options["bind"]
			if !contains {
				bind = "127.0.0.1:8000"
			}
			p := getPersistance(options)
			defer p.Close()
			kvass.RunServer(p, bind)
			return 0
		})

	config_show := cli.NewCommand("show", "print current config").
		WithAction(func(args []string, options map[string]string) int {
			p := getPersistance(options)
			remote := p.State.RemoteHostname
			if remote == "" {
				remote = "(None)"
			}

			fmt.Printf("Encryption Key:  \t%v\n", p.State.Key)
			fmt.Printf("ProcessID:       \t%v\n", p.State.Pid)
			fmt.Printf("Remote:          \t%v\n", remote)
			return 0
		})

	config_key := cli.NewCommand("key", "set encryption key").
		WithArg(cli.NewArg("key", "the hex-encoded enryption key")).
		WithAction(func(args []string, options map[string]string) int {
			key_hex := args[0]
			key, err := hex.DecodeString(strings.TrimSpace(key_hex))
			if err != nil {
				fmt.Println("Error, could not decode supplied key.")
				return 1
			}

			if len(key) != 32 {
				fmt.Println("Error, key has to be 32 bytes long.")
				return 1
			}

			p := getPersistance(options)
			p.State.Key = key_hex
			err = p.CommitState()
			if err != nil {
				fmt.Println("Internal error: ", err.Error)
				return 1
			}

			return 0
		})

	config_pid := cli.NewCommand("pid", "set process id (lower pid wins in case of conflicts").
		WithArg(cli.NewArg("id", "the new process id.").WithType(cli.TypeInt)).
		WithAction(func(args []string, options map[string]string) int {
			pid, err := strconv.Atoi(args[0])
			if err != nil {
				// should never happen, as cli lib does type checking.
				panic(err)
			}

			p := getPersistance(options)
			p.State.Pid = pid
			err = p.CommitState()
			if err != nil {
				fmt.Println("Internal error: ", err.Error)
				return 1
			}

			return 0
		})

	config_remote := cli.NewCommand("remote", "set remote server").
		WithArg(cli.NewArg("host", `example: "1.2.3.4:4242", "" means using no remote`)).
		WithAction(func(args []string, options map[string]string) int {
			host := strings.TrimSpace(args[0])

			p := getPersistance(options)
			p.State.RemoteHostname = host
			err := p.CommitState()
			if err != nil {
				fmt.Println("Internal error: ", err.Error)
				return 1
			}

			return 0
		})

	config := cli.NewCommand("config", "set config parameters").
		WithCommand(config_show).
		WithCommand(config_key).
		WithCommand(config_remote).
		WithCommand(config_pid)

	app := cli.New("kvass - a personal KV store").
		WithOption(cli.NewOption("db", "the database file to use (default: ~/.kvassdb.sqlite")).
		WithCommand(get).
		WithCommand(set).
		WithCommand(config).
		WithCommand(serve)
	os.Exit(app.Run(os.Args, os.Stdout))

}
