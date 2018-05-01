package pogs_test

import (
	"fmt"

	"zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/internal/demo/books"
	"zombiezen.com/go/capnproto2/pogs"
)

var bookData = []byte{
	0x00, 0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0xa0, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x01, 0x00, 0x00, 0x00, 0x72, 0x00, 0x00, 0x00,
	0x57, 0x61, 0x72, 0x20, 0x61, 0x6e, 0x64, 0x20,
	0x50, 0x65, 0x61, 0x63, 0x65, 0x00, 0x00, 0x00,
}

func ExampleExtract() {
	// books.capnp:
	// struct Book {
	//   title @0 :Text;
	//   pageCount @1 :Int32;
	// }

	type Book struct {
		Title     string
		PageCount int32
	}

	// Read the message from bytes.
	msg, err := capnp.Unmarshal(bookData)
	if err != nil {
		panic(err)
	}
	root, err := msg.RootPtr()
	if err != nil {
		panic(err)
	}

	// Extract the book from the root struct.
	b := new(Book)
	if err := pogs.Extract(b, books.Book_TypeID, root.Struct()); err != nil {
		panic(err)
	}
	fmt.Printf("%q has %d pages\n", b.Title, b.PageCount)

	// Output:
	// "War and Peace" has 1440 pages
}

func ExampleInsert() {
	// books.capnp:
	// struct Book {
	//   title @0 :Text;
	//   pageCount @1 :Int32;
	// }

	type Book struct {
		Title     string
		PageCount int32
	}

	// Allocate a new Cap'n Proto Book struct.
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(err)
	}
	root, err := books.NewRootBook(seg)
	if err != nil {
		panic(err)
	}

	// Insert the book struct into the Cap'n Proto struct.
	b := &Book{
		Title:     "War and Peace",
		PageCount: 1440,
	}
	if err := pogs.Insert(books.Book_TypeID, root.Struct, b); err != nil {
		panic(err)
	}
	fmt.Println(root)

	// Output:
	// (title = "War and Peace", pageCount = 1440)
}
