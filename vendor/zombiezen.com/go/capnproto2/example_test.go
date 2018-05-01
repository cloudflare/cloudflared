package capnp_test

import (
	"fmt"
	"io/ioutil"
	"os"

	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
)

func Example() {
	// Make a brand new empty message.
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))

	// If you want runtime-type identification, this is easily obtained. Just
	// wrap everything in a struct that contains a single anoymous union (e.g. struct Z).
	// Then always set a Z as the root object in you message/first segment.
	// The cost of the extra word of storage is usually worth it, as
	// then human readable output is easily obtained via a shell command such as
	//
	// $ cat binary.cpz | capnp decode aircraft.capnp Z
	//
	// If you need to conserve space, and know your content in advance, it
	// isn't necessary to use an anonymous union. Just supply the type name
	// in place of 'Z' in the decode command above.

	// There can only be one root.  Subsequent NewRoot* calls will set the root
	// pointer and orphan the previous root.
	z, err := air.NewRootZ(seg)
	if err != nil {
		panic(err)
	}

	// then non-root objects:
	aircraft, err := z.NewAircraft()
	if err != nil {
		panic(err)
	}
	b737, err := aircraft.NewB737()
	if err != nil {
		panic(err)
	}
	planebase, err := b737.NewBase()
	if err != nil {
		panic(err)
	}

	// Set primitive fields
	planebase.SetCanFly(true)
	planebase.SetName("Henrietta")
	planebase.SetRating(100)
	planebase.SetMaxSpeed(876) // km/hr
	// if we don't set capacity, it will get the default value, in this case 0.
	//planebase.SetCapacity(26020) // Liters fuel

	// Creating a list
	homes, err := planebase.NewHomes(2)
	if err != nil {
		panic(err)
	}
	homes.Set(0, air.Airport_jfk)
	homes.Set(1, air.Airport_lax)

	// Ready to write!

	// You can write to memory...
	buf, err := msg.Marshal()
	if err != nil {
		panic(err)
	}
	_ = buf

	// ... or write to an io.Writer.
	file, err := ioutil.TempFile("", "go-capnproto")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	defer os.Remove(file.Name())
	err = capnp.NewEncoder(file).Encode(msg)
	if err != nil {
		panic(err)
	}
}

func ExampleUnmarshal() {
	msg, s, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		fmt.Printf("allocation error %v\n", err)
		return
	}
	d, err := air.NewRootZdate(s)
	if err != nil {
		fmt.Printf("root error %v\n", err)
		return
	}
	d.SetYear(2004)
	d.SetMonth(12)
	d.SetDay(7)
	data, err := msg.Marshal()
	if err != nil {
		fmt.Printf("marshal error %v\n", err)
		return
	}

	// Read
	msg, err = capnp.Unmarshal(data)
	if err != nil {
		fmt.Printf("unmarshal error %v\n", err)
		return
	}
	d, err = air.ReadRootZdate(msg)
	if err != nil {
		fmt.Printf("read root error %v\n", err)
		return
	}
	fmt.Printf("year %d, month %d, day %d\n", d.Year(), d.Month(), d.Day())
	// Output:
	// year 2004, month 12, day 7
}
