# backoff
## Go implementation of "Exponential Backoff And Jitter"

This package implements the backoff strategy described in the AWS
Architecture Blog article
["Exponential Backoff And Jitter"](http://www.awsarchitectureblog.com/2015/03/backoff.html). Essentially,
the backoff has an interval `time.Duration`; the *n<sup>th</sup>* call
to backoff will return an a `time.Duration` that is *2 <sup>n</sup> *
interval*. If jitter is enabled (which is the default behaviour), the
duration is a random value between 0 and *2 <sup>n</sup> * interval*.
The backoff is configured with a maximum duration that will not be
exceeded; e.g., by default, the longest duration returned is
`backoff.DefaultMaxDuration`.

## Usage

A `Backoff` is initialised with a call to `New`. Using zero values
causes it to use `DefaultMaxDuration` and `DefaultInterval` as the
maximum duration and interval.

```
package something

import "github.com/cloudflare/backoff"

func retryable() {
        b := backoff.New(0, 0)
        for {
                err := someOperation()
                if err == nil {
                    break
                }

                log.Printf("error in someOperation: %v", err)
                <-time.After(b.Duration())
        }

        log.Printf("succeeded after %d tries", b.Tries()+1)
        b.Reset()
}
```

It can also be used to rate limit code that should retry infinitely, but which does not
use `Backoff` itself.

```
package something

import (
    "time"

    "github.com/cloudflare/backoff"
)

func retryable() {
        b := backoff.New(0, 0)
        b.SetDecay(30 * time.Second)

        for {
                // b will reset if someOperation returns later than
                // the last call to b.Duration() + 30s.
                err := someOperation()
                if err == nil {
                    break
                }

                log.Printf("error in someOperation: %v", err)
                <-time.After(b.Duration())
        }
}
```

## Tunables

* `NewWithoutJitter` creates a Backoff that doesn't use jitter.

The default behaviour is controlled by two variables:

* `DefaultInterval` sets the base interval for backoffs created with
  the zero `time.Duration` value in the `Interval` field.
* `DefaultMaxDuration` sets the maximum duration for backoffs created
  with the zero `time.Duration` value in the `MaxDuration` field.

