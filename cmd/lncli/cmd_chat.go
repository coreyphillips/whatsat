// +build routerrpc

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/lightningnetwork/lnd/lntypes"

	"github.com/lightningnetwork/lnd/routing/route"

	"github.com/jroimartin/gocui"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/urfave/cli"
)

var chatCommand = cli.Command{
	Name:      "chat",
	Category:  "Chat",
	ArgsUsage: "recipient_pubkey",
	Usage:     "Use lnd as a p2p messenger application.",
	Action:    actionDecorator(chat),
}

type chatLine struct {
	sender, text string
	delivered    bool
}

var (
	msgLines    []chatLine
	destination *route.Vertex
)

func chat(ctx *cli.Context) error {
	if ctx.NArg() != 0 {
		destHex := ctx.Args().First()
		dest, err := route.NewVertexFromStr(destHex)
		if err != nil {
			return err
		}
		destination = &dest
	}

	conn := getClientConn(ctx, false)
	defer conn.Close()

	client := routerrpc.NewRouterClient(conn)

	req := &routerrpc.ReceiveChatMessagesRequest{}
	rpcCtx := context.Background()
	stream, err := client.ReceiveChatMessages(rpcCtx, req)
	if err != nil {
		return err
	}

	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	g.SetManagerFunc(layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	addMsg := func(sender, text string) int {
		msgLines = append(msgLines, chatLine{
			sender: sender,
			text:   text,
		})

		updateView(g)

		return len(msgLines) - 1
	}

	delivered := func(idx int) {
		msgLines[idx].delivered = true
		updateView(g)
	}

	sendMessage := func(g *gocui.Gui, v *gocui.View) error {
		if len(v.BufferLines()) == 0 {
			return nil
		}
		newMsg := v.BufferLines()[0]

		g.Update(func(g *gocui.Gui) error {
			v.Clear()
			if err := v.SetCursor(0, 0); err != nil {
				return err
			}
			if err := v.SetOrigin(0, 0); err != nil {
				return err
			}
			return nil
		})

		if newMsg[0] == '/' {
			destHex := newMsg[1:]
			dest, err := route.NewVertexFromStr(destHex)
			if err == nil {
				destination = &dest
				v.Title = fmt.Sprintf(" Send to %x ", dest[:4])
			}
			return nil
		}

		if destination == nil {
			return nil
		}

		msgIdx := addMsg("me", newMsg)

		var payHash lntypes.Hash
		if _, err := rand.Read(payHash[:]); err != nil {
			return err
		}

		req := routerrpc.SendPaymentRequest{
			ChatMessage:    newMsg,
			Amt:            100,
			FinalCltvDelta: 40,
			Dest:           destination[:],
			FeeLimitSat:    100,
			TimeoutSeconds: 30,
			PaymentHash:    payHash[:],
		}

		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := client.SendPayment(ctx, &req)
			if err != nil {
				return
			}

			for {
				status, err := stream.Recv()
				if err != nil {
					break
				}

				if status.State == routerrpc.PaymentState_FAILED_INCORRECT_PAYMENT_DETAILS {
					delivered(msgIdx)
					break
				}

				if status.State != routerrpc.PaymentState_IN_FLIGHT {
					break
				}
			}
		}()

		return nil
	}

	err = g.SetKeybinding("send", gocui.KeyEnter, gocui.ModNone, sendMessage)
	if err != nil {
		return err
	}

	go func() {
		for {
			chatMsg, err := stream.Recv()
			if err != nil {
				// return err
			}

			if destination == nil {
				sender, _ := route.NewVertexFromBytes(chatMsg.SenderPubkey)
				destination = &sender
			}

			sender := hex.EncodeToString(chatMsg.SenderPubkey[:4])
			addMsg(sender, chatMsg.Text)
		}
	}()

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}

	return nil
}

func layout(g *gocui.Gui) error {
	g.Cursor = true

	maxX, maxY := g.Size()
	if v, err := g.SetView("messages", 0, 0, maxX-1, maxY-5); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = " Messages "
	}

	if v, err := g.SetView("send", 0, maxY-4, maxX-1, maxY-1); err != nil {
		if _, err := g.SetCurrentView("send"); err != nil {
			return err
		}

		if err != gocui.ErrUnknownView {
			return err
		}

		v.Editable = true
	}

	updateView(g)

	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func updateView(g *gocui.Gui) {
	sendView, _ := g.View("send")
	if destination == nil {
		sendView.Title = " Set a destination by typing /pubkey "
	} else {
		sendView.Title = fmt.Sprintf(" Send to %x ", destination[:4])
	}

	messagesView, _ := g.View("messages")
	g.Update(func(g *gocui.Gui) error {
		messagesView.Clear()
		_, rows := messagesView.Size()

		startLine := len(msgLines) - rows
		if startLine < 0 {
			startLine = 0
		}

		for _, line := range msgLines[startLine:] {
			fmt.Fprintf(messagesView,
				"%8v: %v", line.sender,
				line.text,
			)

			if line.delivered {
				fmt.Fprint(messagesView, "  ✓")
			}

			fmt.Fprintln(messagesView)
		}

		return nil
	})
}
