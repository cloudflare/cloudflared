package capnp_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math/rand"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
	"zombiezen.com/go/capnproto2/internal/capnptool"
)

// A marshalTest tests whether a message can be encoded then read by the
// reference capnp implementation.
type marshalTest struct {
	name string

	msg *capnp.Message
	typ string

	text string
	data []byte
}

func makeMarshalTests(t *testing.T) []marshalTest {
	tests := []marshalTest{
		{
			name: "zdateFilledMessage(1)",
			msg:  zdateFilledMessage(t, 1),
			typ:  "Z",
			text: "(zdatevec = [(year = 2004, month = 12, day = 7)])\n",
		},
		{
			name: "zdateFilledMessage(10)",
			msg:  zdateFilledMessage(t, 10),
			typ:  "Z",
			text: "(zdatevec = [(year = 2004, month = 12, day = 7), (year = 2005, month = 12, day = 7), (year = 2006, month = 12, day = 7), (year = 2007, month = 12, day = 7), (year = 2008, month = 12, day = 7), (year = 2009, month = 12, day = 7), (year = 2010, month = 12, day = 7), (year = 2011, month = 12, day = 7), (year = 2012, month = 12, day = 7), (year = 2013, month = 12, day = 7)])\n",
		},
		{
			name: "zdataFilledMessage(20)",
			msg:  zdataFilledMessage(t, 20),
			typ:  "Z",
			text: `(zdata = (data = "\x00\x01\x02\x03\x04\x05\x06\a\b\t\n\v\f\r\x0e\x0f\x10\x11\x12\x13"))` + "\n",
			data: []byte{
				0, 0, 0, 0, 9, 0, 0, 0,
				0, 0, 0, 0, 3, 0, 1, 0,
				28, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 162, 0, 0, 0,
				0, 1, 2, 3, 4, 5, 6, 7,
				8, 9, 10, 11, 12, 13, 14, 15,
				16, 17, 18, 19, 0, 0, 0, 0,
			},
		},
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := air.NewRootZjob(seg); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, marshalTest{
			name: "empty Zjob",
			msg:  msg,
			typ:  "Zjob",
			text: "()\n",
			data: []byte{
				0, 0, 0, 0, 3, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 2, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		zjob, err := air.NewRootZjob(seg)
		if err != nil {
			t.Fatal(err)
		}
		if err := zjob.SetCmd("abc"); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, marshalTest{
			name: "Zjob with text",
			msg:  msg,
			typ:  "Zjob",
			text: "(cmd = \"abc\")\n",
			data: []byte{
				0, 0, 0, 0, 4, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 2, 0,
				5, 0, 0, 0, 34, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				97, 98, 99, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		zjob, err := air.NewRootZjob(seg)
		if err != nil {
			t.Fatal(err)
		}
		args, err := zjob.NewArgs(1)
		if err != nil {
			t.Fatal(err)
		}
		if err := args.Set(0, "xyz"); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, marshalTest{
			name: "Zjob with text list",
			msg:  msg,
			typ:  "Zjob",
			text: "(args = [\"xyz\"])\n",
			data: []byte{
				0, 0, 0, 0, 5, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 2, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				1, 0, 0, 0, 14, 0, 0, 0,
				1, 0, 0, 0, 34, 0, 0, 0,
				120, 121, 122, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		zjob, err := air.NewRootZjob(seg)
		if err != nil {
			t.Fatal(err)
		}
		if err := zjob.SetCmd("abc"); err != nil {
			t.Fatal(err)
		}
		args, err := zjob.NewArgs(1)
		if err != nil {
			t.Fatal(err)
		}
		if err := args.Set(0, "xyz"); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, marshalTest{
			name: "Zjob with text and text list",
			msg:  msg,
			typ:  "Zjob",
			text: "(cmd = \"abc\", args = [\"xyz\"])\n",
			data: []byte{
				0, 0, 0, 0, 6, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 2, 0,
				5, 0, 0, 0, 34, 0, 0, 0,
				5, 0, 0, 0, 14, 0, 0, 0,
				97, 98, 99, 0, 0, 0, 0, 0,
				1, 0, 0, 0, 34, 0, 0, 0,
				120, 121, 122, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		server, err := air.NewRootZserver(seg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.NewWaitingjobs(1); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "Zserver with one empty job",
			msg:  msg,
			typ:  "Zserver",
			text: "(waitingjobs = [()])\n",
			data: []byte{
				0, 0, 0, 0, 5, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 23, 0, 0, 0,
				4, 0, 0, 0, 0, 0, 2, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		server, err := air.NewRootZserver(seg)
		if err != nil {
			t.Fatal(err)
		}
		joblist, err := server.NewWaitingjobs(1)
		if err != nil {
			t.Fatal(err)
		}
		if err := joblist.At(0).SetCmd("abc"); err != nil {
			t.Fatal(err)
		}
		args, err := joblist.At(0).NewArgs(1)
		if err != nil {
			t.Fatal(err)
		}
		if err := args.Set(0, "xyz"); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "Zserver with one full job",
			msg:  msg,
			typ:  "Zserver",
			text: "(waitingjobs = [(cmd = \"abc\", args = [\"xyz\"])])\n",
			data: []byte{
				0, 0, 0, 0, 8, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 23, 0, 0, 0,
				4, 0, 0, 0, 0, 0, 2, 0,
				5, 0, 0, 0, 34, 0, 0, 0,
				5, 0, 0, 0, 14, 0, 0, 0,
				97, 98, 99, 0, 0, 0, 0, 0,
				1, 0, 0, 0, 34, 0, 0, 0,
				120, 121, 122, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		server, err := air.NewRootZserver(seg)
		if err != nil {
			t.Fatal(err)
		}
		joblist, err := server.NewWaitingjobs(2)
		if err != nil {
			t.Fatal(err)
		}
		if err := joblist.At(0).SetCmd("abc"); err != nil {
			t.Fatal(err)
		}
		if err := joblist.At(1).SetCmd("xyz"); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "Zserver with two jobs",
			msg:  msg,
			typ:  "Zserver",
			text: "(waitingjobs = [(cmd = \"abc\"), (cmd = \"xyz\")])\n",
			data: []byte{
				0, 0, 0, 0, 9, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 39, 0, 0, 0,
				8, 0, 0, 0, 0, 0, 2, 0,
				13, 0, 0, 0, 34, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				9, 0, 0, 0, 34, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				'a', 'b', 'c', 0, 0, 0, 0, 0,
				'x', 'y', 'z', 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		_, scratch, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}

		// in seg
		segbag, err := air.NewRootBag(seg)
		if err != nil {
			t.Fatal(err)
		}

		// in scratch
		xc, err := air.NewRootCounter(scratch)
		if err != nil {
			t.Fatal(err)
		}
		xc.SetSize(9)

		// copy from scratch to seg
		if err = segbag.SetCounter(xc); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "copy struct between messages",
			msg:  msg,
			typ:  "Bag",
			text: "(counter = (size = 9))\n",
			data: []byte{
				0, 0, 0, 0, 6, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				0, 0, 0, 0, 1, 0, 3, 0,
				9, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		_, scratch, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}

		// in seg
		segbag, err := air.NewRootBag(seg)
		if err != nil {
			t.Fatal(err)
		}

		// in scratch
		xc, err := air.NewRootCounter(scratch)
		if err != nil {
			t.Fatal(err)
		}
		xc.SetSize(9)
		if err := xc.SetWords("hello"); err != nil {
			t.Fatal(err)
		}

		// copy from scratch to seg
		if err = segbag.SetCounter(xc); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "copy struct with text between messages",
			msg:  msg,
			typ:  "Bag",
			text: "(counter = (size = 9, words = \"hello\"))\n",
			data: []byte{
				0, 0, 0, 0, 7, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				0, 0, 0, 0, 1, 0, 3, 0,
				9, 0, 0, 0, 0, 0, 0, 0,
				9, 0, 0, 0, 50, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				'h', 'e', 'l', 'l', 'o', 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		_, scratch, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}

		// in seg
		segbag, err := air.NewRootBag(seg)
		if err != nil {
			t.Fatal(err)
		}

		// in scratch
		xc, err := air.NewRootCounter(scratch)
		if err != nil {
			t.Fatal(err)
		}
		xc.SetSize(9)
		wl, err := xc.NewWordlist(2)
		if err != nil {
			t.Fatal(err)
		}
		if err := wl.Set(0, "hello"); err != nil {
			t.Fatal(err)
		}
		if err := wl.Set(1, "bye"); err != nil {
			t.Fatal(err)
		}

		// copy from scratch to seg
		if err = segbag.SetCounter(xc); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "copy struct with list of text between messages",
			msg:  msg,
			typ:  "Bag",
			text: "(counter = (size = 9, wordlist = [\"hello\", \"bye\"]))\n",
			data: []byte{
				0, 0, 0, 0, 10, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				0, 0, 0, 0, 1, 0, 3, 0,
				9, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				5, 0, 0, 0, 22, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				5, 0, 0, 0, 50, 0, 0, 0,
				5, 0, 0, 0, 34, 0, 0, 0,
				'h', 'e', 'l', 'l', 'o', 0, 0, 0,
				'b', 'y', 'e', 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		_, scratch, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}

		// in seg
		segbag, err := air.NewRootBag(seg)
		if err != nil {
			t.Fatal(err)
		}

		// in scratch
		xc, err := air.NewRootCounter(scratch)
		if err != nil {
			t.Fatal(err)
		}
		xc.SetSize(9)
		if err := xc.SetWords("abc"); err != nil {
			t.Fatal(err)
		}
		wl, err := xc.NewWordlist(2)
		if err != nil {
			t.Fatal(err)
		}
		if err := wl.Set(0, "hello"); err != nil {
			t.Fatal(err)
		}
		if err := wl.Set(1, "byenow"); err != nil {
			t.Fatal(err)
		}

		// copy from scratch to seg
		if err = segbag.SetCounter(xc); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "copy struct with data, text, and list of text between messages",
			msg:  msg,
			typ:  "Bag",
			text: "(counter = (size = 9, words = \"abc\", wordlist = [\"hello\", \"byenow\"]))\n",
			data: []byte{
				0, 0, 0, 0, 11, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				0, 0, 0, 0, 1, 0, 3, 0,
				9, 0, 0, 0, 0, 0, 0, 0,
				9, 0, 0, 0, 34, 0, 0, 0,
				9, 0, 0, 0, 22, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				97, 98, 99, 0, 0, 0, 0, 0,
				5, 0, 0, 0, 50, 0, 0, 0,
				5, 0, 0, 0, 58, 0, 0, 0,
				'h', 'e', 'l', 'l', 'o', 0, 0, 0,
				'b', 'y', 'e', 'n', 'o', 'w', 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		_, scratch, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}

		// in seg
		segbag, err := air.NewRootBag(seg)
		if err != nil {
			t.Fatal(err)
		}

		// in scratch
		xc, err := air.NewRootCounter(scratch)
		if err != nil {
			t.Fatal(err)
		}
		bl, err := xc.NewBitlist(3)
		if err != nil {
			t.Fatal(err)
		}
		bl.Set(0, true)
		bl.Set(1, false)
		bl.Set(2, true)

		// copy from scratch to seg
		if err = segbag.SetCounter(xc); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "copy struct with bit list between messages",
			msg:  msg,
			typ:  "Bag",
			text: "(counter = (size = 0, bitlist = [true, false, true]))\n",
			data: []byte{
				0, 0, 0, 0, 7, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				0, 0, 0, 0, 1, 0, 3, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				1, 0, 0, 0, 25, 0, 0, 0,
				5, 0, 0, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		holder, err := air.NewRootHoldsVerEmptyList(seg)
		if err != nil {
			t.Fatal(err)
		}
		elist, err := air.NewVerEmpty_List(seg, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := holder.SetMylist(elist); err != nil {
			t.Fatal(err)
		}

		tests = append(tests, marshalTest{
			name: "V0 list of empty",
			msg:  msg,
			typ:  "HoldsVerEmptyList",
			text: "(mylist = [(), ()])\n",
			data: []byte{
				0, 0, 0, 0, 3, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 7, 0, 0, 0,
				8, 0, 0, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		holder, err := air.NewRootNester1Capn(seg)
		if err != nil {
			t.Fatal(err)
		}
		initNester(t, holder, "furiosa", "max")

		tests = append(tests, marshalTest{
			name: "list inside a struct",
			msg:  msg,
			typ:  "Nester1Capn",
			text: "(strs = [\"furiosa\", \"max\"])\n",
			data: []byte{
				0, 0, 0, 0, 6, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 22, 0, 0, 0,
				5, 0, 0, 0, 66, 0, 0, 0,
				5, 0, 0, 0, 34, 0, 0, 0,
				102, 117, 114, 105, 111, 115, 97, 0,
				109, 97, 120, 0, 0, 0, 0, 0,
			},
		})
	}

	{
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Fatal(err)
		}
		holder, err := air.NewRootRWTestCapn(seg)
		if err != nil {
			t.Fatal(err)
		}
		mat, err := capnp.NewPointerList(seg, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := holder.SetNestMatrix(mat); err != nil {
			t.Fatal(err)
		}

		row0, err := air.NewNester1Capn_List(seg, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := mat.Set(0, row0); err != nil {
			t.Fatal(err)
		}
		initNester(t, row0.At(0), "z", "w")
		initNester(t, row0.At(1), "q", "r")

		row1, err := air.NewNester1Capn_List(seg, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := mat.Set(1, row1); err != nil {
			t.Fatal(err)
		}
		initNester(t, row1.At(0), "zebra", "wally")
		initNester(t, row1.At(1), "qubert", "rocks")

		tests = append(tests, marshalTest{
			name: "doubly-nested list of struct that has a list field",
			msg:  msg,
			typ:  "RWTestCapn",
			text: `(nestMatrix = [[(strs = ["z", "w"]), (strs = ["q", "r"])], [(strs = ["zebra", "wally"]), (strs = ["qubert", "rocks"])]])` + "\n",
			data: []byte{
				0, 0, 0, 0, 26, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 1, 0,
				1, 0, 0, 0, 22, 0, 0, 0,
				5, 0, 0, 0, 23, 0, 0, 0,
				45, 0, 0, 0, 23, 0, 0, 0,
				8, 0, 0, 0, 0, 0, 1, 0,
				5, 0, 0, 0, 22, 0, 0, 0,
				17, 0, 0, 0, 22, 0, 0, 0,
				5, 0, 0, 0, 18, 0, 0, 0,
				5, 0, 0, 0, 18, 0, 0, 0,
				122, 0, 0, 0, 0, 0, 0, 0,
				119, 0, 0, 0, 0, 0, 0, 0,
				5, 0, 0, 0, 18, 0, 0, 0,
				5, 0, 0, 0, 18, 0, 0, 0,
				113, 0, 0, 0, 0, 0, 0, 0,
				114, 0, 0, 0, 0, 0, 0, 0,
				8, 0, 0, 0, 0, 0, 1, 0,
				5, 0, 0, 0, 22, 0, 0, 0,
				17, 0, 0, 0, 22, 0, 0, 0,
				5, 0, 0, 0, 50, 0, 0, 0,
				5, 0, 0, 0, 50, 0, 0, 0,
				122, 101, 98, 114, 97, 0, 0, 0,
				119, 97, 108, 108, 121, 0, 0, 0,
				5, 0, 0, 0, 58, 0, 0, 0,
				5, 0, 0, 0, 50, 0, 0, 0,
				113, 117, 98, 101, 114, 116, 0, 0,
				114, 111, 99, 107, 115, 0, 0, 0,
			},
		})
	}

	return tests
}

func TestMarshalShouldMatchData(t *testing.T) {
	t.Parallel()
	for _, test := range makeMarshalTests(t) {
		if test.data == nil {
			// TODO(light): backfill all data
			continue
		}
		data, err := test.msg.Marshal()
		if err != nil {
			t.Errorf("%s: marshal error: %v", test.name, err)
			continue
		}
		want, err := encodeTestMessage(test.typ, test.text, test.data)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if !bytes.Equal(data, want) {
			t.Errorf("%s: Marshal returned:\n%s\nwant:\n%s", test.name, hex.Dump(data), hex.Dump(want))
		}
	}
}

func TestMarshalShouldMatchTextWhenDecoded(t *testing.T) {
	t.Parallel()
	tool, err := capnptool.Find()
	if err != nil {
		t.Skip("capnp tool not found:", err)
	}
	for _, test := range makeMarshalTests(t) {
		data, err := test.msg.Marshal()
		if err != nil {
			t.Errorf("%s: marshal error: %v", test.name, err)
			continue
		}
		text, err := tool.Decode(capnptool.Type{SchemaPath: schemaPath, Name: test.typ}, bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s: capnp decode: %v", test.name, err)
			continue
		}
		if text != test.text {
			t.Errorf("%s: decoded to:\n%q; want:\n%q", test.name, text, test.text)
		}
	}
}

func TestMarshalPackedShouldMatchTextWhenDecoded(t *testing.T) {
	t.Parallel()
	tool, err := capnptool.Find()
	if err != nil {
		t.Skip("capnp tool not found:", err)
	}
	for _, test := range makeMarshalTests(t) {
		data, err := test.msg.MarshalPacked()
		if err != nil {
			t.Errorf("%s: marshal error: %v", test.name, err)
			continue
		}
		text, err := tool.DecodePacked(capnptool.Type{SchemaPath: schemaPath, Name: test.typ}, bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s: capnp decode: %v", test.name, err)
			continue
		}
		if text != test.text {
			t.Errorf("%s: decoded to:\n%q; want:\n%q", test.name, text, test.text)
		}
	}
}

type bitListTest struct {
	list []bool
	text string
}

var bitListTests = []bitListTest{
	{
		[]bool{true, false, true},
		"(boolvec = [true, false, true])\n",
	},
	{
		[]bool{false},
		"(boolvec = [false])\n",
	},
	{
		[]bool{true},
		"(boolvec = [true])\n",
	},
	{
		[]bool{false, true},
		"(boolvec = [false, true])\n",
	},
	{
		[]bool{true, true},
		"(boolvec = [true, true])\n",
	},
	{
		[]bool{false, false, true},
		"(boolvec = [false, false, true])\n",
	},
	{
		[]bool{true, false, true, false, true},
		"(boolvec = [true, false, true, false, true])\n",
	},
	{
		[]bool{
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			false, false, false, false, false, false, false, false,
			true, true,
		},
		"(boolvec = [false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true, true])\n",
	},
}

func (blt bitListTest) makeMessage() (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		return nil, err
	}
	list, err := capnp.NewBitList(seg, int32(len(blt.list)))
	if err != nil {
		return nil, err
	}
	for i := range blt.list {
		list.Set(i, blt.list[i])
	}
	if err := z.SetBoolvec(list); err != nil {
		return nil, err
	}
	return msg, nil
}

func TestBitList(t *testing.T) {
	t.Parallel()
	for _, test := range bitListTests {
		msg, err := test.makeMessage()
		if err != nil {
			t.Errorf("%v: make message: %v", test.list, err)
			continue
		}

		z, err := air.ReadRootZ(msg)
		if err != nil {
			t.Errorf("%v: read root Z: %v", test.list, err)
			continue
		}
		if w := z.Which(); w != air.Z_Which_boolvec {
			t.Errorf("%v: root.Which() = %v; want boolvec", test.list, w)
			continue
		}
		list, err := z.Boolvec()
		if err != nil {
			t.Errorf("%v: read Z.boolvec: %v", test.list, err)
			continue
		}
		if n := list.Len(); n != len(test.list) {
			t.Errorf("%v: len(Z.boolvec) = %d; want %d", test.list, n, len(test.list))
			continue
		}
		for i := range test.list {
			if li := list.At(i); li != test.list[i] {
				t.Errorf("%v: Z.boolvec[%d] = %t; want %t", test.list, i, li, test.list[i])
			}
		}
	}
}

func TestBitList_Decode(t *testing.T) {
	t.Parallel()
	tool, err := capnptool.Find()
	if err != nil {
		t.Skip("capnp tool not found:", err)
	}
	for _, test := range bitListTests {
		msg, err := test.makeMessage()
		if err != nil {
			t.Errorf("%v: make message: %v", test.list, err)
			continue
		}
		out, err := msg.Marshal()
		if err != nil {
			t.Errorf("%v: marshal: %v", test.list, err)
			continue
		}
		text, err := tool.Decode(capnptool.Type{SchemaPath: schemaPath, Name: "Z"}, bytes.NewReader(out))
		if err != nil {
			t.Errorf("%v: capnp decode: %v", test.list, err)
			continue
		}
		if text != test.text {
			t.Errorf("%v: capnp decode = %q; want %q", test.list, text, test.text)
		}
	}
}

func TestZDataAccessors(t *testing.T) {
	t.Parallel()
	data := mustEncodeTestMessage(t, "Z", `(zdata = (data = "\x00\x01\x02\x03\x04\x05\x06\a\b\t\n\v\f\r\x0e\x0f\x10\x11\x12\x13"))`, []byte{
		0, 0, 0, 0, 9, 0, 0, 0,
		0, 0, 0, 0, 3, 0, 1, 0,
		28, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 162, 0, 0, 0,
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 0, 0, 0, 0,
	})

	msg, err := capnp.Unmarshal(data)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}
	z, err := air.ReadRootZ(msg)
	if err != nil {
		t.Fatal("ReadRootZ:", err)
	}

	if z.Which() != air.Z_Which_zdata {
		t.Fatalf("z.Which() = %v; want zdata", z.Which())
	}
	zdata, err := z.Zdata()
	if err != nil {
		t.Fatal("z.Zdata():", err)
	}
	d, err := zdata.Data()
	if err != nil {
		t.Fatal("z.Zdata().Data():", err)
	}
	if len(d) != 20 {
		t.Errorf("z.Zdata().Data() len = %d; want 20", len(d))
	}
	for i := range d {
		if d[i] != byte(i) {
			t.Errorf("z.Zdata().Data()[%d] = %d; want %d", i, d[i], i)
		}
	}
}

func TestInterfaceSet(t *testing.T) {
	t.Parallel()
	cl := air.Echo{Client: capnp.ErrorClient(errors.New("foo"))}
	_, s, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	base, err := air.NewRootEchoBase(s)
	if err != nil {
		t.Fatal(err)
	}

	base.SetEcho(cl)

	if base.Echo() != cl {
		t.Errorf("base.Echo() = %#v; want %#v", base.Echo(), cl)
	}
}

func TestInterfaceSetNull(t *testing.T) {
	t.Parallel()
	cl := air.Echo{Client: capnp.ErrorClient(errors.New("foo"))}
	msg, s, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	base, err := air.NewRootEchoBase(s)
	if err != nil {
		t.Fatal(err)
	}
	base.SetEcho(cl)

	base.SetEcho(air.Echo{})

	if e := base.Echo().Client; e != nil {
		t.Errorf("base.Echo() = %#v; want nil", e)
	}
	if len(msg.CapTable) != 1 {
		t.Errorf("msg.CapTable = %#v; want len = 1", msg.CapTable)
	}
}

func TestInterfaceCopyToOtherMessage(t *testing.T) {
	t.Parallel()
	cl := air.Echo{Client: capnp.ErrorClient(errors.New("foo"))}
	_, s1, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	base1, err := air.NewRootEchoBase(s1)
	if err != nil {
		t.Fatal(err)
	}
	if err := base1.SetEcho(cl); err != nil {
		t.Fatal(err)
	}

	_, s2, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	hoth2, err := air.NewRootHoth(s2)
	if err != nil {
		t.Fatal(err)
	}
	if err := hoth2.SetBase(base1); err != nil {
		t.Fatal(err)
	}

	if base, err := hoth2.Base(); err != nil {
		t.Errorf("hoth2.Base() error: %v", err)
	} else if base.Echo() != cl {
		t.Errorf("hoth2.Base().Echo() = %#v; want %#v", base.Echo(), cl)
	}
	tab2 := s2.Message().CapTable
	if len(tab2) == 1 {
		if tab2[0] != cl.Client {
			t.Errorf("s2.Message().CapTable[0] = %#v; want %#v", tab2[0], cl.Client)
		}
	} else {
		t.Errorf("len(s2.Message().CapTable) = %d; want 1", len(tab2))
	}
}

func TestInterfaceCopyToOtherMessageWithCaps(t *testing.T) {
	t.Parallel()
	cl := air.Echo{Client: capnp.ErrorClient(errors.New("foo"))}
	_, s1, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	base1, err := air.NewRootEchoBase(s1)
	if err != nil {
		t.Fatal(err)
	}
	if err := base1.SetEcho(cl); err != nil {
		t.Fatal(err)
	}

	_, s2, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	s2.Message().AddCap(nil)
	hoth2, err := air.NewRootHoth(s2)
	if err != nil {
		t.Fatal(err)
	}
	if err := hoth2.SetBase(base1); err != nil {
		t.Fatal(err)
	}

	if base, err := hoth2.Base(); err != nil {
		t.Errorf("hoth2.Base() error: %v", err)
	} else if base.Echo() != cl {
		t.Errorf("hoth2.Base().Echo() = %#v; want %#v", base.Echo(), cl)
	}
	tab2 := s2.Message().CapTable
	if len(tab2) != 2 {
		t.Errorf("len(s2.Message().CapTable) = %d; want 2", len(tab2))
	}
}

func TestReadListInStruct(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(t, "Nester1Capn", "(strs = [\"furiosa\", \"max\"])", []byte{
		0, 0, 0, 0, 6, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 22, 0, 0, 0,
		5, 0, 0, 0, 66, 0, 0, 0,
		5, 0, 0, 0, 34, 0, 0, 0,
		102, 117, 114, 105, 111, 115, 97, 0,
		109, 97, 120, 0, 0, 0, 0, 0,
	})
	msg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}
	holder, err := air.ReadRootNester1Capn(msg)
	if err != nil {
		t.Fatal("ReadRootNester1Capn:", err)
	}
	strs, err := holder.Strs()
	if err != nil {
		t.Fatal("Nester1Capn.strs:", err)
	}
	if strs.Len() == 2 {
		check := func(i int, want string) {
			s, err := strs.At(i)
			if err != nil {
				t.Errorf("Nester1Capn.strs[%d] error: %v", i, err)
			}
			if s != want {
				t.Errorf("Nester1Capn.strs[%d] = %q; want %q", i, s, want)
			}
		}
		check(0, "furiosa")
		check(1, "max")
	} else {
		t.Errorf("len(Nester1Capn.strs) = %d; want 2", strs.Len())
	}
}

func TestReadNestedListOfStructWithList(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(
		t,
		"RWTestCapn",
		`(nestMatrix = [[(strs = ["z", "w"]), (strs = ["q", "r"])], [(strs = ["zebra", "wally"]), (strs = ["qubert", "rocks"])]])`,
		[]byte{
			0, 0, 0, 0, 26, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 1, 0,
			1, 0, 0, 0, 22, 0, 0, 0,
			5, 0, 0, 0, 23, 0, 0, 0,
			45, 0, 0, 0, 23, 0, 0, 0,
			8, 0, 0, 0, 0, 0, 1, 0,
			5, 0, 0, 0, 22, 0, 0, 0,
			17, 0, 0, 0, 22, 0, 0, 0,
			5, 0, 0, 0, 18, 0, 0, 0,
			5, 0, 0, 0, 18, 0, 0, 0,
			122, 0, 0, 0, 0, 0, 0, 0,
			119, 0, 0, 0, 0, 0, 0, 0,
			5, 0, 0, 0, 18, 0, 0, 0,
			5, 0, 0, 0, 18, 0, 0, 0,
			113, 0, 0, 0, 0, 0, 0, 0,
			114, 0, 0, 0, 0, 0, 0, 0,
			8, 0, 0, 0, 0, 0, 1, 0,
			5, 0, 0, 0, 22, 0, 0, 0,
			17, 0, 0, 0, 22, 0, 0, 0,
			5, 0, 0, 0, 50, 0, 0, 0,
			5, 0, 0, 0, 50, 0, 0, 0,
			122, 101, 98, 114, 97, 0, 0, 0,
			119, 97, 108, 108, 121, 0, 0, 0,
			5, 0, 0, 0, 58, 0, 0, 0,
			5, 0, 0, 0, 50, 0, 0, 0,
			113, 117, 98, 101, 114, 116, 0, 0,
			114, 111, 99, 107, 115, 0, 0, 0,
		})
	msg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}
	holder, err := air.ReadRootRWTestCapn(msg)
	if err != nil {
		t.Fatal("ReadRootRWTestCapn:", err)
	}
	mat, err := holder.NestMatrix()
	if err != nil {
		t.Fatal("RWTestCapn:", err)
	}
	check := func(i, j int, want ...string) {
		if i >= mat.Len() {
			t.Errorf("len(RWTestCapn.nestMatrix) = %d; tried to index %d", mat.Len(), i)
			return
		}
		row, err := mat.At(i)
		if err != nil {
			t.Errorf("RWTestCapn.nestMatrix[%d]: %v", i, err)
			return
		}
		rowList := air.Nester1Capn_List{List: capnp.ToList(row)}
		if j >= rowList.Len() {
			t.Errorf("len(RWTestCapn.nestMatrix[%d]) = %d; tried to index %d", i, rowList.Len(), j)
			return
		}
		strs, err := rowList.At(j).Strs()
		if err != nil {
			t.Errorf("RWTestCapn.nestMatrix[%d][%d].strs: %v", i, j, err)
			return
		}
		if strs.Len() != len(want) {
			t.Errorf("len(RWTestCapn.nestMatrix[%d][%d].strs) = %d; want %d", i, j, strs.Len(), len(want))
			return
		}
		for k := 0; k < strs.Len(); k++ {
			s, err := strs.At(k)
			if err != nil {
				t.Errorf("RWTestCapn.nestMatrix[%d][%d].strs[%d]: %v", i, j, k, err)
				continue
			}
			if s != want[k] {
				t.Errorf("RWTestCapn.nestMatrix[%d][%d].strs[%d] = %q; want %q", i, j, k, s, want[k])
			}
		}
	}
	check(0, 0, "z", "w")
	check(0, 1, "q", "r")
	check(1, 0, "zebra", "wally")
	check(1, 1, "qubert", "rocks")
}

func TestDataVersioningAvoidsUnnecessaryTruncation(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(t, "VerTwoDataTwoPtr", "(val = 9, duo = 8, ptr1 = (val = 77), ptr2 = (val = 55))", []byte{
		0, 0, 0, 0, 7, 0, 0, 0,
		0, 0, 0, 0, 2, 0, 2, 0,
		9, 0, 0, 0, 0, 0, 0, 0,
		8, 0, 0, 0, 0, 0, 0, 0,
		4, 0, 0, 0, 1, 0, 0, 0,
		4, 0, 0, 0, 1, 0, 0, 0,
		77, 0, 0, 0, 0, 0, 0, 0,
		55, 0, 0, 0, 0, 0, 0, 0,
	})
	want := mustEncodeTestMessage(t, "Wrap2x2", "(mightNotBeReallyEmpty = (val = 9, duo = 8, ptr1 = (val = 77), ptr2 = (val = 55)))", []byte{
		0, 0, 0, 0, 8, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		0, 0, 0, 0, 2, 0, 2, 0,
		9, 0, 0, 0, 0, 0, 0, 0,
		8, 0, 0, 0, 0, 0, 0, 0,
		4, 0, 0, 0, 1, 0, 0, 0,
		4, 0, 0, 0, 1, 0, 0, 0,
		77, 0, 0, 0, 0, 0, 0, 0,
		55, 0, 0, 0, 0, 0, 0, 0,
	})

	msg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}

	// Read in the message as if it's an old client (less fields in schema).
	oldRoot, err := air.ReadRootVerEmpty(msg)
	if err != nil {
		t.Fatal("ReadRootVerEmpty:", err)
	}

	// Store the larger message into another segment.
	freshMsg, freshSeg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	wrapEmpty, err := air.NewRootWrapEmpty(freshSeg)
	if err != nil {
		t.Fatal("NewRootWrapEmpty:", err)
	}
	if err := wrapEmpty.SetMightNotBeReallyEmpty(oldRoot); err != nil {
		t.Fatal("SetMightNotBeReallyEmpty:", err)
	}

	// Verify that it matches the expected serialization.
	out, err := freshMsg.Marshal()
	if err != nil {
		t.Fatal("Marshal:", err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("After copy, data is:\n%s\nwant:\n%s", hex.Dump(out), hex.Dump(want))
	}
}

func TestZserverAccessors(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(t, "Zserver", `(waitingjobs = [(cmd = "abc"), (cmd = "xyz")])`, []byte{
		0, 0, 0, 0, 9, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 39, 0, 0, 0,
		8, 0, 0, 0, 0, 0, 2, 0,
		13, 0, 0, 0, 34, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		9, 0, 0, 0, 34, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		97, 98, 99, 0, 0, 0, 0, 0,
		120, 121, 122, 0, 0, 0, 0, 0,
	})

	msg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}

	zserver, err := air.ReadRootZserver(msg)
	if err != nil {
		t.Fatal("ReadRootZserver:", err)
	}
	joblist, err := zserver.Waitingjobs()
	if err != nil {
		t.Fatal("Zserver.waitingjobs:", err)
	}
	if joblist.Len() != 2 {
		t.Fatalf("len(Zserver.waitingjobs) = %d; want 2", joblist.Len())
	}
	checkCmd := func(i int, want string) {
		cmd, err := joblist.At(i).Cmd()
		if err != nil {
			t.Errorf("Zserver.waitingjobs[%d].cmd error: %v", i, err)
			return
		}
		if cmd != want {
			t.Errorf("Zserver.waitingjobs[%d].cmd = %q; want %q", i, cmd, want)
		}
	}
	checkCmd(0, "abc")
	checkCmd(1, "xyz")
}

func TestEnumFromString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s  string
		ap air.Airport
	}{
		{"jfk", air.Airport_jfk},
		{"notEverMatching", 0},
	}
	for _, test := range tests {
		if ap := air.AirportFromString(test.s); ap != test.ap {
			t.Errorf("air.AirportFromString(%q) = %v; want %v", test.s, ap, test.ap)
		}
	}
}

func TestDefaultStructField(t *testing.T) {
	t.Parallel()
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	root, err := air.NewRootStackingRoot(seg)
	if err != nil {
		t.Fatal(err)
	}

	a, err := root.AWithDefault()

	if err != nil {
		t.Error("StackingRoot.aWithDefault error:", err)
	}
	if a.Num() != 42 {
		t.Errorf("StackingRoot.aWithDefault = %d; want 42", a.Num())
	}
}

func TestDataTextCopyOptimization(t *testing.T) {
	t.Parallel()
	_, seg1, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	root, err := air.NewRootNester1Capn(seg1)
	if err != nil {
		t.Fatal(err)
	}
	_, seg2, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	strsl, err := capnp.NewTextList(seg2, 256)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < strsl.Len(); i++ {
		strsl.Set(i, "testess")
	}

	err = root.SetStrs(strsl)

	if err != nil {
		t.Fatal(err)
	}
	strsl, err = root.Strs()
	if err != nil {
		t.Fatal(err)
	}
	if strsl.Len() != 256 {
		t.Errorf("strsl.Len() = %d; want 256", strsl.Len())
	}
	for i := 0; i < strsl.Len(); i++ {
		s, err := strsl.At(i)
		if err != nil {
			t.Errorf("strsl.At(%d) error: %v", i, err)
			continue
		}
		if s != "testess" {
			t.Errorf("strsl.At(%d) = %q; want \"testess\"", i, s)
		}
	}
}

// highlight how much faster text movement between segments
// is when special casing Text and Data
//
// run this test with capnp.go:1334-1341 commented in/out to compare.
//
func BenchmarkTextMovementBetweenSegments(b *testing.B) {
	buf := make([]byte, 1<<21)
	buf2 := make([]byte, 1<<21)

	text := make([]byte, 1<<20)
	for i := range text {
		text[i] = byte(65 + rand.Int()%26)
	}

	astr := make([]string, 1000)
	for i := range astr {
		astr[i] = string(text[i*1000 : (i+1)*1000])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, seg, _ := capnp.NewMessage(capnp.SingleSegment(buf[:0]))
		_, scratch, _ := capnp.NewMessage(capnp.SingleSegment(buf2[:0]))

		ht, _ := air.NewRootHoldsText(seg)
		// Purposefully created in another segment.
		tl, _ := capnp.NewTextList(scratch, 1000)
		for j := 0; j < 1000; j++ {
			tl.Set(j, astr[j])
		}

		ht.SetLst(tl)
	}
}

func TestV1DataVersioningBiggerToEmpty(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(t, "HoldsVerTwoDataList", "(mylist = [(val = 27, duo = 26), (val = 42, duo = 41)])", []byte{
		0, 0, 0, 0, 7, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 39, 0, 0, 0,
		8, 0, 0, 0, 2, 0, 0, 0,
		27, 0, 0, 0, 0, 0, 0, 0,
		26, 0, 0, 0, 0, 0, 0, 0,
		42, 0, 0, 0, 0, 0, 0, 0,
		41, 0, 0, 0, 0, 0, 0, 0,
	})

	remsg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}

	// 0 data
	func() {
		reHolder0, err := air.ReadRootHoldsVerEmptyList(remsg)
		if err != nil {
			t.Error("ReadRootHoldsVerEmptyList:", err)
			return
		}
		list0, err := reHolder0.Mylist()
		if err != nil {
			t.Error("HoldsVerEmptyList.mylist:", err)
			return
		}
		if list0.Len() != 2 {
			t.Errorf("len(HoldsVerEmptyList.mylist) = %d; want 2", list0.Len())
		}
	}()

	// 1 datum
	func() {
		reHolder1, err := air.ReadRootHoldsVerOneDataList(remsg)
		if err != nil {
			t.Error("ReadRootHoldsVerOneDataList:", err)
			return
		}
		list1, err := reHolder1.Mylist()
		if err != nil {
			t.Error("HoldsVerOneDataList.mylist:", err)
			return
		}
		if list1.Len() == 2 {
			if v := list1.At(0).Val(); v != 27 {
				t.Errorf("HoldsVerOneDataList.mylist[0].val = %d; want 27", v)
			}
			if v := list1.At(1).Val(); v != 42 {
				t.Errorf("HoldsVerOneDataList.mylist[1].val = %d; want 42", v)
			}
		} else {
			t.Errorf("len(HoldsVerOneDataList.mylist) = %d; want 2", list1.Len())
		}
	}()

	// 2 data
	func() {
		reHolder2, err := air.ReadRootHoldsVerTwoDataList(remsg)
		if err != nil {
			t.Error("ReadRootHoldsVerTwoDataList:", err)
			return
		}
		list2, err := reHolder2.Mylist()
		if err != nil {
			t.Error("HoldsVerTwoDataList.mylist:", err)
			return
		}
		if list2.Len() == 2 {
			if v := list2.At(0).Val(); v != 27 {
				t.Errorf("HoldsVerTwoDataList.mylist[0].val = %d; want 27", v)
			}
			if v := list2.At(0).Duo(); v != 26 {
				t.Errorf("HoldsVerTwoDataList.mylist[0].duo = %d; want 26", v)
			}
			if v := list2.At(1).Val(); v != 42 {
				t.Errorf("HoldsVerTwoDataList.mylist[1].val = %d; want 42", v)
			}
			if v := list2.At(1).Duo(); v != 41 {
				t.Errorf("HoldsVerTwoDataList.mylist[1].duo = %d; want 41", v)
			}
		} else {
			t.Errorf("len(HoldsVerTwoDataList.mylist) = %d; want 2", list2.Len())
		}
	}()
}

func TestV1DataVersioningEmptyToBigger(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(t, "HoldsVerEmptyList", "(mylist = [(),()])", []byte{
		0, 0, 0, 0, 3, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 7, 0, 0, 0,
		8, 0, 0, 0, 0, 0, 0, 0,
	})

	remsg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}

	reHolder1, err := air.ReadRootHoldsVerOneDataList(remsg)
	if err != nil {
		t.Fatal("ReadRootHoldsVerOneDataList:", err)
	}
	list1, err := reHolder1.Mylist()
	if err != nil {
		t.Fatal("HoldsVerOneDataList.mylist:", err)
	}
	if list1.Len() == 2 {
		if v := list1.At(0).Val(); v != 0 {
			t.Errorf("HoldsVerOneDataList.mylist[0].val = %d; want 0", v)
		}
		if v := list1.At(1).Val(); v != 0 {
			t.Errorf("HoldsVerOneDataList.mylist[1].val = %d; want 0", v)
		}
	} else {
		t.Errorf("len(HoldsVerOneDataList.mylist) = %d; want 2", list1.Len())
	}

	reHolder2, err := air.ReadRootHoldsVerTwoDataList(remsg)
	if err != nil {
		t.Fatal("ReadRootHoldsVerOneDataList:", err)
	}
	list2, err := reHolder2.Mylist()
	if err != nil {
		t.Fatal("HoldsVerOneDataList.mylist:", err)
	}
	if list2.Len() == 2 {
		if v := list2.At(0).Val(); v != 0 {
			t.Errorf("HoldsVerTwoDataList.mylist[0].val = %d; want 0", v)
		}
		if v := list2.At(0).Duo(); v != 0 {
			t.Errorf("HoldsVerTwoDataList.mylist[0].duo = %d; want 0", v)
		}
		if v := list2.At(1).Val(); v != 0 {
			t.Errorf("HoldsVerTwoDataList.mylist[1].val = %d; want 0", v)
		}
		if v := list2.At(1).Duo(); v != 0 {
			t.Errorf("HoldsVerTwoDataList.mylist[1].duo = %d; want 0", v)
		}
	} else {
		t.Errorf("len(HoldsVerTwoDataList.mylist) = %d; want 2", list2.Len())
	}
}

func TestDataVersioningZeroPointersToMore(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(t, "HoldsVerEmptyList", "(mylist = [(),()])", []byte{
		0, 0, 0, 0, 3, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 7, 0, 0, 0,
		8, 0, 0, 0, 0, 0, 0, 0,
	})

	remsg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}
	reHolder, err := air.ReadRootHoldsVerTwoTwoList(remsg)
	if err != nil {
		t.Fatal("ReadRootHoldsVerTwoTwoList:", err)
	}
	list22, err := reHolder.Mylist()
	if err != nil {
		t.Fatal("HoldsVerTwoTwoList.mylist:", err)
	}
	if list22.Len() != 2 {
		t.Errorf("len(HoldsVerTwoTwoList.mylist) = %d; want 2", list22.Len())
	}
	for i := 0; i < list22.Len(); i++ {
		ele := list22.At(i)
		if val := ele.Val(); val != 0 {
			t.Errorf("HoldsVerTwoTwoList.mylist[%d].val = %d; want 0", i, val)
		}
		if duo := ele.Duo(); duo != 0 {
			t.Errorf("HoldsVerTwoTwoList.mylist[%d].duo = %d; want 0", i, duo)
		}
		if ptr1, err := ele.Ptr1(); err != nil {
			t.Errorf("HoldsVerTwoTwoList.mylist[%d].ptr1: %v", i, err)
		} else if capnp.IsValid(ptr1) {
			t.Errorf("HoldsVerTwoTwoList.mylist[%d].ptr1 = %#v; want invalid (nil)", i, ptr1)
		}
		if ptr2, err := ele.Ptr2(); err != nil {
			t.Errorf("HoldsVerTwoTwoList.mylist[%d].ptr2: %v", i, err)
		} else if capnp.IsValid(ptr2) {
			t.Errorf("HoldsVerTwoTwoList.mylist[%d].ptr2 = %#v; want invalid (nil)", i, ptr2)
		}
	}
}

func TestDataVersioningZeroPointersToTwo(t *testing.T) {
	t.Parallel()
	in := mustEncodeTestMessage(
		t,
		"HoldsVerTwoTwoList",
		`(mylist = [
			(val = 27, duo = 26, ptr1 = (val = 25), ptr2 = (val = 23)),
			(val = 42, duo = 41, ptr1 = (val = 40), ptr2 = (val = 38))])`,
		[]byte{
			0, 0, 0, 0, 15, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 1, 0,
			1, 0, 0, 0, 71, 0, 0, 0,
			8, 0, 0, 0, 2, 0, 2, 0,
			27, 0, 0, 0, 0, 0, 0, 0,
			26, 0, 0, 0, 0, 0, 0, 0,
			20, 0, 0, 0, 1, 0, 0, 0,
			20, 0, 0, 0, 1, 0, 0, 0,
			42, 0, 0, 0, 0, 0, 0, 0,
			41, 0, 0, 0, 0, 0, 0, 0,
			12, 0, 0, 0, 1, 0, 0, 0,
			12, 0, 0, 0, 1, 0, 0, 0,
			25, 0, 0, 0, 0, 0, 0, 0,
			23, 0, 0, 0, 0, 0, 0, 0,
			40, 0, 0, 0, 0, 0, 0, 0,
			38, 0, 0, 0, 0, 0, 0, 0,
		})

	remsg, err := capnp.Unmarshal(in)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}

	// 0 pointers
	func() {
		reHolder, err := air.ReadRootHoldsVerEmptyList(remsg)
		if err != nil {
			t.Error("ReadRootHoldsVerEmptyList:", err)
			return
		}
		list, err := reHolder.Mylist()
		if err != nil {
			t.Error("HoldsVerEmptyList.mylist:", err)
			return
		}
		if list.Len() != 2 {
			t.Errorf("len(HoldsVerEmptyList.mylist) = %d; want 2", list.Len())
		}
	}()

	// 1 pointer
	func() {
		holder, err := air.ReadRootHoldsVerOnePtrList(remsg)
		if err != nil {
			t.Error("ReadRootHoldsVerOnePtrList:", err)
			return
		}
		list, err := holder.Mylist()
		if err != nil {
			t.Error("HoldsVerOnePtrList.mylist:", err)
			return
		}
		if list.Len() != 2 {
			t.Errorf("len(HoldsVerOnePtrList.mylist) = %d; want 2", list.Len())
			return
		}
		check := func(i int, val int16) {
			p, err := list.At(i).Ptr()
			if err != nil {
				t.Errorf("HoldsVerOnePtrList.mylist[%d].ptr: %v", i, err)
				return
			}
			if p.Val() != val {
				t.Errorf("HoldsVerOnePtrList.mylist[%d].ptr.val = %d; want %d", i, p.Val(), val)
			}
		}
		check(0, 25)
		check(1, 40)
	}()

	// 2 pointers
	func() {
		holder, err := air.ReadRootHoldsVerTwoTwoPlus(remsg)
		if err != nil {
			t.Error("ReadRootHoldsVerTwoTwoPlus:", err)
			return
		}
		list, err := holder.Mylist()
		if err != nil {
			t.Error("HoldsVerTwoTwoPlus.mylist:", err)
			return
		}
		if list.Len() != 2 {
			t.Errorf("len(HoldsVerTwoTwoPlus.mylist) = %d; want 2", list.Len())
			return
		}
		check := func(i int, val1, val2 int16) {
			if p, err := list.At(i).Ptr1(); err != nil {
				t.Errorf("HoldsVerTwoTwoPlus.mylist[%d].ptr1: %v", i, err)
			} else if p.Val() != val1 {
				t.Errorf("HoldsVerTwoTwoPlus.mylist[%d].ptr1.val = %d; want %d", i, p.Val(), val1)
			}
			if p, err := list.At(i).Ptr2(); err != nil {
				t.Errorf("HoldsVerTwoTwoPlus.mylist[%d].ptr2: %v", i, err)
			} else if p.Val() != val2 {
				t.Errorf("HoldsVerTwoTwoPlus.mylist[%d].ptr2.val = %d; want %d", i, p.Val(), val2)
			}
		}
		check(0, 25, 23)
		check(1, 40, 38)
	}()
}

func TestVoidUnionSetters(t *testing.T) {
	t.Parallel()
	want := mustEncodeTestMessage(t, "VoidUnion", "(b = void)", []byte{
		0, 0, 0, 0, 2, 0, 0, 0,
		0, 0, 0, 0, 1, 0, 0, 0,
		1, 0, 0, 0, 0, 0, 0, 0,
	})

	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	voidUnion, err := air.NewRootVoidUnion(seg)
	if err != nil {
		t.Fatal(err)
	}
	voidUnion.SetB()

	act, err := msg.Marshal()
	if err != nil {
		t.Fatal("msg.Marshal():", err)
	}
	if !bytes.Equal(act, want) {
		t.Errorf("msg.Marshal() =\n%s\n; want:\n%s", hex.Dump(act), hex.Dump(want))
	}
}

func TestReadDefaults(t *testing.T) {
	t.Parallel()
	data := mustEncodeTestMessage(t, "Defaults", "()", []byte{
		0, 0, 0, 0, 5, 0, 0, 0,
		0, 0, 0, 0, 2, 0, 2, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
	})

	msg, err := capnp.Unmarshal(data)
	if err != nil {
		t.Fatal("Unmarshal:", err)
	}
	d, err := air.ReadRootDefaults(msg)
	if err != nil {
		t.Fatal("ReadRootDefaults:", err)
	}

	if s, err := d.Text(); err != nil {
		t.Errorf("d.Text() error: %v", err)
	} else if s != "foo" {
		t.Errorf("d.Text() = %q; want \"foo\"", s)
	}
	if b, err := d.TextBytes(); err != nil {
		t.Errorf("d.TextBytes() error: %v", err)
	} else if !bytes.Equal(b, []byte("foo")) {
		t.Errorf("d.TextBytes() = %q; want \"foo\"", b)
	}
	if b, err := d.Data(); err != nil {
		t.Errorf("d.Data() error: %v", err)
	} else if !bytes.Equal(b, []byte("bar")) {
		t.Errorf("d.Data() = %q; want \"bar\"", b)
	}
	if f := d.Float(); f != 3.14 {
		t.Errorf("d.Float() = %g; want 3.14", f)
	}
	if i := d.Int(); i != -123 {
		t.Errorf("d.Int() = %d; want -123", i)
	}
	if i := d.Uint(); i != 42 {
		t.Errorf("d.Uint() = %d; want 42", i)
	}
}

type A struct {
	Name     string
	BirthDay time.Time
	Phone    string
	Siblings int
	Spouse   bool
	Money    float64
}

func generateA(r *rand.Rand) *A {
	return &A{
		Name:     randString(r, 16),
		BirthDay: time.Unix(r.Int63(), 0),
		Phone:    randString(r, 10),
		Siblings: r.Intn(5),
		Spouse:   r.Intn(2) == 1,
		Money:    r.Float64(),
	}
}

func unmarshalA(aa air.BenchmarkA) A {
	name, _ := aa.NameBytes()
	phone, _ := aa.PhoneBytes()
	return A{
		Name:     unsafeBytesToString(name),
		BirthDay: time.Unix(aa.BirthDay(), 0),
		Phone:    unsafeBytesToString(phone),
		Siblings: int(aa.Siblings()),
		Spouse:   aa.Spouse(),
		Money:    aa.Money(),
	}
}

func (a *A) fill(aa air.BenchmarkA) {
	aa.SetName(a.Name)
	aa.SetBirthDay(a.BirthDay.Unix())
	aa.SetPhone(a.Phone)
	aa.SetSiblings(int32(a.Siblings))
	aa.SetSpouse(a.Spouse)
	aa.SetMoney(a.Money)
}

func randString(r *rand.Rand, n int) string {
	b := make([]byte, (n+1)/2)
	// Go 1.6 adds a Rand.Read method, but since we want to be compatible with Go 1.4...
	for i := range b {
		b[i] = byte(r.Intn(255))
	}
	return hex.EncodeToString(b)[:n]
}

func unsafeBytesToString(b []byte) string {
	slice := *(*reflect.SliceHeader)(unsafe.Pointer(&b))
	hdr := reflect.StringHeader{Data: slice.Data, Len: slice.Len}
	return *(*string)(unsafe.Pointer(&hdr))
}

func BenchmarkMarshal(b *testing.B) {
	r := rand.New(rand.NewSource(12345))
	data := make([]*A, 1000)
	for i := range data {
		data[i] = generateA(r)
	}
	arena := make([]byte, 0, 512)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := data[r.Intn(len(data))]
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(arena[:0]))
		root, _ := air.NewRootBenchmarkA(seg)
		a.fill(root)
		msg.Marshal()
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	r := rand.New(rand.NewSource(12345))
	data := make([][]byte, 1000)
	for i := range data {
		a := generateA(r)
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
		root, _ := air.NewRootBenchmarkA(seg)
		a.fill(root)
		data[i], _ = msg.Marshal()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, _ := capnp.Unmarshal(data[r.Intn(len(data))])
		a, _ := air.ReadRootBenchmarkA(msg)
		unmarshalA(a)
	}
}

func BenchmarkUnmarshal_Reuse(b *testing.B) {
	r := rand.New(rand.NewSource(12345))
	data := make([][]byte, 1000)
	for i := range data {
		a := generateA(r)
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
		root, _ := air.NewRootBenchmarkA(seg)
		a.fill(root)
		data[i], _ = msg.Marshal()
	}
	msg := new(capnp.Message)
	ta := new(testArena)
	arena := capnp.Arena(ta)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		*ta = testArena(data[r.Intn(len(data))][8:])
		msg.Reset(arena)
		a, _ := air.ReadRootBenchmarkA(msg)
		unmarshalA(a)
	}
}

func BenchmarkDecode(b *testing.B) {
	var buf bytes.Buffer

	r := rand.New(rand.NewSource(12345))
	enc := capnp.NewEncoder(&buf)
	count := 10000

	for i := 0; i < count; i++ {
		a := generateA(r)
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
		root, _ := air.NewRootBenchmarkA(seg)
		a.fill(root)
		enc.Encode(msg)
	}

	blob := buf.Bytes()

	b.ReportAllocs()
	b.SetBytes(int64(buf.Len()))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dec := capnp.NewDecoder(bytes.NewReader(blob))

		for {
			msg, err := dec.Decode()

			if err == io.EOF {
				break
			}

			if err != nil {
				b.Fatal(err)
			}

			_, err = air.ReadRootBenchmarkA(msg)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkDecode_Reuse(b *testing.B) {
	var buf bytes.Buffer

	r := rand.New(rand.NewSource(12345))
	enc := capnp.NewEncoder(&buf)
	count := 10000

	for i := 0; i < count; i++ {
		a := generateA(r)
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
		root, _ := air.NewRootBenchmarkA(seg)
		a.fill(root)
		enc.Encode(msg)
	}

	blob := buf.Bytes()

	b.ReportAllocs()
	b.SetBytes(int64(buf.Len()))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dec := capnp.NewDecoder(bytes.NewReader(blob))
		dec.ReuseBuffer()

		for {
			msg, err := dec.Decode()

			if err == io.EOF {
				break
			}

			if err != nil {
				b.Fatal(err)
			}

			_, err = air.ReadRootBenchmarkA(msg)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

type testArena []byte

func (ta testArena) NumSegments() int64 {
	return 1
}

func (ta testArena) Data(id capnp.SegmentID) ([]byte, error) {
	if id != 0 {
		return nil, errors.New("test arena: requested non-zero segment")
	}
	return []byte(ta), nil
}

func (ta testArena) Allocate(capnp.Size, map[capnp.SegmentID]*capnp.Segment) (capnp.SegmentID, []byte, error) {
	return 0, nil, errors.New("test arena: can't allocate")
}

func TestPointerTraverseDefense(t *testing.T) {
	t.Parallel()
	const limit = 128
	msg := &capnp.Message{
		Arena: capnp.SingleSegment([]byte{
			0, 0, 0, 0, 1, 0, 0, 0, // root 1-word struct pointer to next word
			0, 0, 0, 0, 0, 0, 0, 0, // struct's data
		}),
		TraverseLimit: limit * 8,
	}

	for i := 0; i < limit; i++ {
		_, err := msg.RootPtr()
		if err != nil {
			t.Fatalf("iteration %d RootPtr: %v", i, err)
		}
	}

	if _, err := msg.RootPtr(); err == nil {
		t.Fatalf("deref %d did not fail as expected", limit+1)
	}
}

func TestPointerDepthDefense(t *testing.T) {
	t.Parallel()
	const limit = 64
	msg := &capnp.Message{
		Arena: capnp.SingleSegment([]byte{
			0, 0, 0, 0, 0, 0, 1, 0, // root 1-pointer struct pointer to next word
			0xfc, 0xff, 0xff, 0xff, 0, 0, 1, 0, // root struct pointer that points back to itself
		}),
		DepthLimit: limit,
	}
	root, err := msg.Root()
	if err != nil {
		t.Fatal("Root:", err)
	}

	curr := capnp.ToStruct(root)
	if !capnp.IsValid(curr) {
		t.Fatal("Root is not a struct")
	}
	for i := 0; i < limit-1; i++ {
		p, err := curr.Pointer(0)
		if err != nil {
			t.Fatalf("deref %d fail: %v", i+1, err)
		}
		if !capnp.IsValid(p) {
			t.Fatalf("deref %d is invalid", i+1)
		}
		curr = capnp.ToStruct(p)
		if !capnp.IsValid(curr) {
			t.Fatalf("deref %d is not a struct", i+1)
		}
	}

	_, err = curr.Pointer(0)
	if err == nil {
		t.Fatalf("deref %d did not fail as expected", limit)
	}
}

func TestPointerDepthDefenseAcrossStructsAndLists(t *testing.T) {
	t.Parallel()
	const limit = 63
	msg := &capnp.Message{
		Arena: capnp.SingleSegment([]byte{
			0, 0, 0, 0, 0, 0, 1, 0, // root 1-pointer struct pointer to next word
			0x01, 0, 0, 0, 0x0e, 0, 0, 0, // list pointer to 1-element list of pointer (next word)
			0xf8, 0xff, 0xff, 0xff, 0, 0, 1, 0, // struct pointer to previous word
		}),
		DepthLimit: limit,
	}

	toStruct := func(p capnp.Pointer, err error) (capnp.Struct, error) {
		if err != nil {
			return capnp.Struct{}, err
		}
		if !capnp.IsValid(p) {
			return capnp.Struct{}, errors.New("invalid pointer")
		}
		s := capnp.ToStruct(p)
		if !capnp.IsValid(s) {
			return capnp.Struct{}, errors.New("not a struct")
		}
		return s, nil
	}
	toList := func(p capnp.Pointer, err error) (capnp.List, error) {
		if err != nil {
			return capnp.List{}, err
		}
		if !capnp.IsValid(p) {
			return capnp.List{}, errors.New("invalid pointer")
		}
		l := capnp.ToList(p)
		if !capnp.IsValid(l) {
			return capnp.List{}, errors.New("not a list")
		}
		return l, nil
	}
	curr, err := toStruct(msg.Root())
	if err != nil {
		t.Fatal("Root:", err)
	}
	for i := limit; i > 2; {
		l, err := toList(curr.Pointer(0))
		if err != nil {
			t.Fatalf("deref %d (for list): %v", limit-i+1, err)
		}
		i--
		curr, err = toStruct(capnp.PointerList{List: l}.At(0))
		if err != nil {
			t.Fatalf("deref %d (for struct): %v", limit-i+1, err)
		}
		i--
	}

	_, err = curr.Pointer(0)
	if err == nil {
		t.Fatalf("deref %d did not fail as expected", limit)
	}
}

func TestHasPointerInUnion(t *testing.T) {
	t.Parallel()
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	craft, err := air.NewRootAircraft(seg)
	if err != nil {
		t.Fatal("NewRootAircraft:", err)
	}
	t.Log("NewB737")
	_, err = craft.NewB737()
	if err != nil {
		t.Fatal("NewB737:", err)
	}

	// These pointers are at the same offset.
	if !craft.HasB737() {
		t.Errorf("HasB737 = false; want true")
	}
	if craft.HasA320() {
		t.Errorf("HasA320 = true; want false")
	}
}

func TestSetNilBlob(t *testing.T) {
	t.Parallel()
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatal("NewRootZ:", err)
	}
	if err := z.SetBlob(nil); err != nil {
		t.Fatal("z.SetBlob(nil):", err)
	}

	if z.HasBlob() {
		t.Error("z.HasBlob() = true; want false")
	}
	blob, err := z.Blob()
	if err != nil {
		t.Errorf("z.Blob(): %v", err)
	}
	if blob != nil {
		t.Errorf("z.Blob() = %v; want nil", blob)
	}
}

func TestSetEmptyText(t *testing.T) {
	t.Parallel()
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatal("NewRootZ:", err)
	}
	if err := z.SetText(""); err != nil {
		t.Fatal("z.SetText(\"\"):", err)
	}

	if z.HasText() {
		t.Error("z.HasText() = true; want false")
	}
	text, err := z.Text()
	if err != nil {
		t.Errorf("z.Text(): %v", err)
	}
	if text != "" {
		t.Errorf("z.Text() = %q; want \"\"", text)
	}
	b, err := z.TextBytes()
	if err != nil {
		t.Errorf("z.TextBytes(): %v", err)
	}
	if b != nil {
		t.Errorf("z.TextBytes() = %v; want nil", b)
	}
}

func TestSetNilBlobWithDefault(t *testing.T) {
	t.Parallel()
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	d, err := air.NewRootDefaults(seg)
	if err != nil {
		t.Fatal("NewRootDefaults:", err)
	}
	if err := d.SetData(nil); err != nil {
		t.Fatal("d.SetData(nil):", err)
	}

	if !d.HasData() {
		t.Error("d.HasData() = false; want true")
	}
	blob, err := d.Data()
	if err != nil {
		t.Errorf("d.Data(): %v", err)
	}
	if len(blob) != 0 {
		t.Errorf("d.Data() = %v; want zero length", blob)
	}
	// Specifically not checking for nil.  Anything zero-length is appropriate here.
}

func TestSetEmptyTextWithDefault(t *testing.T) {
	t.Parallel()
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	d, err := air.NewRootDefaults(seg)
	if err != nil {
		t.Fatal("NewRootDefaults:", err)
	}
	if err := d.SetText(""); err != nil {
		t.Fatal("d.SetData(\"\"):", err)
	}

	if !d.HasText() {
		t.Error("d.HasText() = false; want true")
	}
	text, err := d.Text()
	if err != nil {
		t.Errorf("d.Text(): %v", err)
	}
	if text != "" {
		t.Errorf("d.Text() = %v; want zero length", text)
	}
	b, err := d.TextBytes()
	if err != nil {
		t.Errorf("d.TextBytes(): %v", err)
	}
	if len(b) != 0 {
		t.Errorf("d.TextBytes() = %v; want zero length", b)
	}
}

func TestFuzzedListOutOfBounds(t *testing.T) {
	t.Parallel()
	msg := &capnp.Message{
		Arena: capnp.SingleSegment([]byte(
			"\x00\x00\x00\x00\x03\x00\x01\x00\x0f\x000000000000" +
				"000000000000\x01\x00\x00\x00\x13\x00\x00\x000\x00\x00\x00\x00\x00\x00\x00")),
	}
	z, err := air.ReadRootZ(msg)
	if err != nil {
		t.Fatal("ReadRootZ:", err)
	}
	if z.Which() != air.Z_Which_f64vec {
		t.Fatalf("z.Which() = %v; want Z_Which_f64vec", z.Which())
	}
	v, err := z.F64vec()
	if err != nil {
		t.Fatal("z.F64vec:", err)
	}
	for i := 0; i < v.Len(); i++ {
		// This should not crash.
		t.Logf("v.At(%d); v.Len() = %d", i, v.Len())
		v.At(i)
	}
}

func benchmarkGrowth(b *testing.B, newArena func() capnp.Arena) {
	const (
		fieldValue = "1234567" // carefully chosen to be word-padded

		rootMessageOverhead = 8 * 3 // root pointer, Document struct, composite list tag
		perFieldOverhead    = 8 * 2 // Field struct, fieldValue + "\0"
		numElements         = 64 * 1024
		totalSize           = rootMessageOverhead + perFieldOverhead*numElements
	)
	b.SetBytes(totalSize)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, seg, err := capnp.NewMessage(newArena())
		if err != nil {
			b.Fatal(err)
		}
		doc, err := air.NewRootAllocBenchmark(seg)
		if err != nil {
			b.Fatal(err)
		}
		d, err := doc.NewFields(numElements)
		if err != nil {
			b.Fatal(err)
		}
		for j := 0; j < numElements; j++ {
			if err := d.At(j).SetStringValue(fieldValue); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkGrowth_SingleSegment(b *testing.B) {
	benchmarkGrowth(b, func() capnp.Arena { return capnp.SingleSegment(nil) })
}

func BenchmarkGrowth_MultiSegment(b *testing.B) {
	benchmarkGrowth(b, func() capnp.Arena { return capnp.MultiSegment(nil) })
}

func benchmarkSmallMessage(b *testing.B, newArena func() capnp.Arena) {
	const fieldValue = "1234567" // carefully chosen to be word-padded
	b.SetBytes(8 * 9)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, seg, err := capnp.NewMessage(newArena())
		if err != nil {
			b.Fatal(err)
		}
		root, err := capnp.NewRootStruct(seg, capnp.ObjectSize{PointerCount: 2})
		if err != nil {
			b.Fatal(err)
		}

		sub1, err := capnp.NewStruct(root.Segment(), capnp.ObjectSize{PointerCount: 1})
		if err != nil {
			b.Fatal(err)
		}
		if err := root.SetPtr(0, sub1.ToPtr()); err != nil {
			b.Fatal(err)
		}
		text, err := capnp.NewText(sub1.Segment(), fieldValue)
		if err != nil {
			b.Fatal(err)
		}
		if err := sub1.SetPtr(0, text.ToPtr()); err != nil {
			b.Fatal(err)
		}

		sub2, err := capnp.NewStruct(root.Segment(), capnp.ObjectSize{DataSize: 32})
		if err != nil {
			b.Fatal(err)
		}
		if err := root.SetPtr(0, sub2.ToPtr()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSmallMessage_SingleSegment(b *testing.B) {
	benchmarkSmallMessage(b, func() capnp.Arena { return capnp.SingleSegment(nil) })
}

func BenchmarkSmallMessage_MultiSegment(b *testing.B) {
	benchmarkSmallMessage(b, func() capnp.Arena { return capnp.MultiSegment(nil) })
}
