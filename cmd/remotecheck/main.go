// Command remotecheck exercises Flint Remote (Settings ▸ Remote) from the
// command line, the way the phone app will: pair with the code Flint shows,
// list chats and tasks, and follow the event stream.
//
//	go run ./cmd/remotecheck -url http://127.0.0.1:7419 pair ABCD-EFGH-IJKL-MNOP
//	export FLINT_REMOTE_TOKEN=…        # printed by pair
//	go run ./cmd/remotecheck -url http://127.0.0.1:7419 chats
//	go run ./cmd/remotecheck -url http://127.0.0.1:7419 tasks
//	go run ./cmd/remotecheck -url http://127.0.0.1:7419 follow [chatId:seq …]
//
// The token is read from FLINT_REMOTE_TOKEN, never from the command line
// (other users can read process arguments).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"flint/internal/remote/client"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:7419", "Flint Remote's address")
	name := flag.String("name", "remotecheck", "device name to pair as")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: remotecheck [-url URL] pair CODE | chats | tasks | follow [chatId:seq …]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *base, *name, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "remotecheck:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, base, name string, args []string) error {
	if args[0] == "pair" {
		if len(args) != 2 {
			return errors.New("pair needs the code Flint shows")
		}
		token, dev, err := client.Pair(ctx, nil, base, args[1], name)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "paired as %s (%s), scopes %v\n", dev.Name, dev.ID, dev.Scopes)
		fmt.Printf("export FLINT_REMOTE_TOKEN=%s\n", token)
		return nil
	}
	token := os.Getenv("FLINT_REMOTE_TOKEN")
	if token == "" {
		return errors.New("FLINT_REMOTE_TOKEN is not set (run pair first)")
	}
	c := &client.Client{Base: base, Token: token}
	out := json.NewEncoder(os.Stdout)
	switch args[0] {
	case "chats":
		chats, err := c.Chats(ctx)
		if err != nil {
			return err
		}
		return out.Encode(chats)
	case "tasks":
		tasks, err := c.Tasks(ctx)
		if err != nil {
			return err
		}
		return out.Encode(tasks)
	case "follow":
		since, err := parseSince(args[1:])
		if err != nil {
			return err
		}
		return follow(ctx, c, since, out)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func parseSince(args []string) (map[string]uint64, error) {
	since := map[string]uint64{}
	for _, a := range args {
		i := strings.LastIndexByte(a, ':')
		if i <= 0 {
			return nil, fmt.Errorf("since %q: want chatId:seq", a)
		}
		seq, err := strconv.ParseUint(a[i+1:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("since %q: %w", a, err)
		}
		since[a[:i]] = seq
	}
	return since, nil
}

// follow prints every stream message as one JSON line until the stream or
// the command ends; it reconnects with the last Seq per chat when the
// server asks it to (it fell behind).
func follow(ctx context.Context, c *client.Client, since map[string]uint64, out *json.Encoder) error {
	for {
		s, err := c.Stream(ctx, since)
		if err != nil {
			return err
		}
		stop := context.AfterFunc(ctx, func() { _ = s.Close() })
		for {
			m, err := s.Next()
			if err != nil {
				stop()
				_ = s.Close()
				var ce *client.CloseError
				if errors.As(err, &ce) && ce.Code == 1013 {
					time.Sleep(time.Second)
					break // reconnect with since
				}
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if m.Type == "agent" && m.Event != nil {
				since[m.Event.SessionID] = m.Event.Seq
			}
			if err := out.Encode(m); err != nil {
				return err
			}
		}
	}
}
