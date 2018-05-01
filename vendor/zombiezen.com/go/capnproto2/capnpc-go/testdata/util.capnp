# Sandstorm - Personal Cloud Sandbox
# Copyright (c) 2014 Sandstorm Development Group, Inc. and contributors
# All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Derived from sandstorm at 08fafe55995edfe188777eb00b5a2d1826032c59.
# Using Go annotations.

@0xecd50d792c3d9992;

using Go = import "go.capnp";
$Go.package("util");
$Go.import("zombiezen.com/go/capnproto2/capnpc-go/testdata/util");

using DateInNs = Int64;
using DurationInNs = UInt64;

struct KeyValue {
  key @0 :Text;
  value @1 :Text;
}

struct LocalizedText {
  # Text intended to be displayed to a user.  May be localized to multiple languages.
  #
  # TODO(soon):  Maybe instead of packing all translations in here, we should have a message code
  #   and parameter substitutions, with the (message code, locale) -> text map stored elsewhere?

  defaultText @0 :Text;
  # What to display if no localization matching the user's preferences is available.

  localizations @1 :List(Localization);
  # Localized versions of the text.

  struct Localization {
    locale @0 :Text;  # IETF BCP 47 locale, e.g. "en" or "en-US".
    text @1 :Text;    # Localized text.
  }
}

interface Handle {
  # Arbitrary handle to some resource provided by the platform. May or may not be persistent,
  # depending on the use case.
  #
  # To "drop" a handle means to discard any references. The purpose of a handle is to detect when
  # it has been dropped and to free the underlying resource and cancel any ongoing operation at
  # that time.
  #
  # A handle can be persistent. Once you have called `save()` on it to obtain a SturdyRef, dropping
  # the live reference will not cancel the operation. You must drop all live references *and*
  # explicitly drop any SturdyRef. Every interface which supports restoring SturdyRefs also
  # has a corresponding `drop()` method for this purpose.
  #
  # Unfortunately, there is no way to ensure that a SturdyRef will eventually be deleted. A grain
  # could, for example, call `save()` and then simply fail to store the SturdyRef anywhere, causing
  # it to be "leaked" until such a time as the grain itself is destroyed. Or worse, a whole server
  # could be destroyed in a fire, leaking all SturdyRefs stored therein forever. Apps implementing
  # persistent handles must be designed to account for this, probably by giving the owning user
  # a way to inspect incoming references and remove them manually. Sandstorm automatically provides
  # such an interface for all apps it hosts.
}

interface ByteStream {
  # Represents a destination for a stream of bytes. The bytes are ordered, but boundaries between
  # messages are not semantically important.
  #
  # Streams are push-oriented (traditionally, "output streams") rather than pull-oriented ("input
  # streams") because this most easily allows multiple packets to be in-flight at once while
  # allowing flow control at either end. If we tried to design a pull-oriented stream, it would
  # suffer from problems:
  # * If we used a naive read() method that returns a simple data blob, you would need to make
  #   multiple simultaneous calls to deal with network latency. However, those calls could
  #   potentially return in the wrong order. Although you could restore order by keeping track of
  #   the order in which the calls were made, this would be a lot of work, and most callers would
  #   probably fail to do it.
  # * We could instead have a read() method that returns a blob as well as a capability to read the
  #   next blob. You would then make multiple calls using pipelining. Even in this case, though,
  #   an unpredictable event loop could schedule a pipelined call's return before the parent call.
  #   Moreover, the interface would be awkward to use and implement. E.g. what happens if you call
  #   read() twice on the same capability?

  write @0 (data :Data);
  # Add bytes.
  #
  # It's safe to make overlapping calls to `write()`, since Cap'n Proto enforces E-Order and so
  # the calls will be delivered in order. However, it is a good idea to limit how much data is
  # in-flight at a time, so that it doesn't fill up buffers and block other traffic. On the other
  # hand, having only one `write()` in flight at a time will not fully utilize the available
  # bandwidth if the connection has any significant latency, so parallelizing a few `write()`s is
  # a good idea.
  #
  # Similarly, the implementation of `ByteStream` can delay returning from `write()` as a way to
  # hint to the caller that it should hold off on further writes.

  done @1 ();
  # Call after the last write to indicate that there is no more data. If the `ByteStream` is
  # discarded without a call to `done()`, the callee must assume that an error occurred and that
  # the data is incomplete.
  #
  # This will not return until all bytes are successfully written to their final destination.
  # It will throw an exception if any error occurs, including if the total number of bytes written
  # did not match `expectSize()`.

  expectSize @2 (size :UInt64);
  # Optionally called to let the receiver know exactly how much data will be written. This should
  # normally be called before the first write(), but if called later, `size` indicates how many
  # more bytes to expect _after_ the call. It is an error by the caller if more or fewer bytes are
  # actually written before `done()`; this also implies that all calls to `expectSize()` must be
  # consistent. The caller will ignore any exceptions thrown from this method, therefore it
  # is not necessary for the callee to actually implement it.
}

interface Blob @0xe53527a75d90198f {
  # Represents a large byte blob.

  getSize @0 () -> (size :UInt64);
  # Get the total size of the blob. May block if the blob is still being uploaded and the size is
  # not yet known.

  writeTo @1 (stream :ByteStream, startAtOffset :UInt64 = 0) -> (handle :Handle);
  # Write the contents of the blob to `stream`.

  getSlice @2 (offset :UInt64, size :UInt32) -> (data :Data);
  # Read a slice of the blob starting at the given offset. `size` cannot be greater than Cap'n
  # Proto's limit of 2^29-1, and reasonable servers will likely impose far lower limits. If the
  # slice would cross past the end of the blob, it is truncated. Otherwise, `data` is always
  # exactly `size` bytes (though the caller should check for security purposes).
  #
  # One technique that makes a lot of sense is to start off by calling e.g. `getSlice(0, 65536)`.
  # If the returned data is less than 65536 bytes then you know you got the whole blob, otherwise
  # you may want to switch to `writeTo`.
}

interface Assignable(T) {
  # An "assignable" -- a mutable memory cell. Supports subscribing to updates.

  get @0 () -> (value :T, setter :Setter);
  # The returned setter functions the same as you'd get from `asSetter()` except that it will
  # become disconnected the next time the Assignable is set by someone else. Thus, you may use this
  # to implement optimistic concurrency control.

  asGetter @1 () -> (getter :Getter);
  # Return a read-only capability for this assignable, co-hosted with the assignable itself for
  # performance.  If the assignable is persistent, the getter is as well.

  asSetter @2 () -> (setter :Setter);
  # Return a write-only capability for this assignable, co-hosted with the assignable itself for
  # performance.  If the assignable is persistent, the setter is as well.

  interface Getter {
    get @0 () -> (value :T);

    subscribe @1 (setter :Setter) -> (handle :Handle);
    # Subscribe to updates. Calls the given setter any time the assignable's value changes.  Drop
    # the returned handle to stop receiving updates. If `setter` is persistent, `handle` will also
    # be persistent.
  }

  interface Setter {
    set @0 (value :T) -> ();
  }
}
