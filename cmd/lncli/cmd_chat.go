// +build routerrpc

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

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
	fee          uint64
}

var (
	msgLines       []chatLine
	destination    *route.Vertex
	runningBalance map[route.Vertex]int64 = make(map[route.Vertex]int64)
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

	addMsg := func(line chatLine) int {
		msgLines = append(msgLines, line)

		updateView(g)

		return len(msgLines) - 1
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

		msgIdx := addMsg(chatLine{
			sender: "me",
			text:   newMsg,
		})

		payAmt := runningBalance[*destination]
		if payAmt < 1000 {
			payAmt = 1000
		}

		req := routerrpc.SendPaymentRequest{
			ChatMessage:    newMsg,
			AmtMsat:        payAmt,
			FinalCltvDelta: 40,
			Dest:           destination[:],
			FeeLimitMsat:   10000,
			TimeoutSeconds: 30,
		}

		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := client.SendPayment(ctx, &req)
			if err != nil {
				g.Update(func(g *gocui.Gui) error {
					return err
				})
				return
			}

			for {
				status, err := stream.Recv()
				if err != nil {
					break
				}

				switch status.State {
				case routerrpc.PaymentState_SUCCEEDED:
					msgLines[msgIdx].fee = uint64(status.Route.TotalFeesMsat)
					runningBalance[*destination] -= payAmt
					fallthrough

				case routerrpc.PaymentState_FAILED_INCORRECT_PAYMENT_DETAILS:
					msgLines[msgIdx].delivered = true
					updateView(g)
					break

				case routerrpc.PaymentState_IN_FLIGHT:

				default:
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
				g.Update(func(g *gocui.Gui) error {
					return err
				})
				return
			}

			if destination == nil {
				sender, _ := route.NewVertexFromBytes(chatMsg.SenderPubkey)
				destination = &sender
			}

			sender := hex.EncodeToString(chatMsg.SenderPubkey[:4])
			addMsg(chatLine{
				sender: sender,
				text:   chatMsg.Text,
			})

			runningBalance[*destination] += chatMsg.AmtReceivedMsat
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
		sendView.Title = fmt.Sprintf(" Send to %x [balance: %v msat]",
			destination[:4], runningBalance[*destination])
	}

	messagesView, _ := g.View("messages")
	g.Update(func(g *gocui.Gui) error {
		messagesView.Clear()
		cols, rows := messagesView.Size()

		startLine := len(msgLines) - rows
		if startLine < 0 {
			startLine = 0
		}

		for _, line := range msgLines[startLine:] {
			text := line.text

			var amtDisplay string
			if line.delivered {
				amtDisplay = formatMsat(line.fee)
			}

			maxTextFieldLen := cols - len(amtDisplay) - 12
			maxTextLen := maxTextFieldLen
			if line.delivered {
				maxTextLen -= 2
			}
			if len(text) > maxTextLen {
				text = text[:maxTextLen-3] + "..."
			}
			paddingLen := maxTextFieldLen - len(text)
			if line.delivered {
				text += " \x1b[34m✔️\x1b[0m"
				paddingLen -= 2
			}

			text += strings.Repeat(" ", paddingLen)

			fmt.Fprintf(messagesView, "%8v: %v \x1b[34m%v\x1b[0m",
				line.sender,
				text, amtDisplay,
			)

			fmt.Fprintln(messagesView)
		}

		return nil
	})
}

func formatMsat(msat uint64) string {
	wholeSats := msat / 1000
	msats := msat % 1000
	var msatsStr string
	if msats > 0 {
		msatsStr = fmt.Sprintf(".%03d", msats)
		msatsStr = strings.TrimRight(msatsStr, "0")
	}
	return fmt.Sprintf("[%d%-4s sat]",
		wholeSats, msatsStr,
	)
}
