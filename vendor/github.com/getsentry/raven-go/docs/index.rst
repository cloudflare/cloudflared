.. sentry:edition:: self

    Raven Go
    ========

.. sentry:edition:: hosted, on-premise

    .. class:: platform-go

    Go
    ==

.. sentry:support-warning::

    The Go SDK is maintained and supported by Sentry but currently
    under development.  Learn more about the project on `GitHub
    <https://github.com/getsentry/raven-go>`__.


Raven-Go provides a Sentry client implementation for the Go programming
language.

Installation
------------

Raven-Go can be installed like any other Go library through ``go get``::

    $ go get github.com/getsentry/raven-go

Configuring the Client
----------------------

To use ``raven-go``, you'll need to import the ``raven`` package, then initilize your
DSN globally. If you specify the ``SENTRY_DSN`` environment variable, this will be
done automatically for you. The release and environment can also be specified in the
environment variables ``SENTRY_RELEASE`` and ``SENTRY_ENVIRONMENT`` respectively.

.. sourcecode:: go

    package main

    import "github.com/getsentry/raven-go"

    func init() {
        raven.SetDSN("___DSN___")
    }

Reporting Errors
----------------

In Go, there are both errors and panics, and Raven can handle both. To learn more
about the differences, please read `Error handling and Go <https://blog.golang.org/error-handling-and-go>`_.

To handle normal ``error`` responses, we have two options: ``CaptureErrorAndWait`` and ``CaptureError``. The former is a blocking call, for a case where you'd like to exit the application after reporting, and the latter is non-blocking.

.. sourcecode:: go

    f, err := os.Open("filename.ext")
    if err != nil {
        raven.CaptureErrorAndWait(err, nil)
        log.Panic(err)
    }

Reporting Panics
----------------

Capturing a panic is pretty simple as well. We just need to wrap our code in ``CapturePanic``. ``CapturePanic`` will execute the ``func`` and if a panic happened, we will record it, and gracefully continue.

.. sourcecode:: go

    raven.CapturePanic(func() {
        // do all of the scary things here
    }, nil)


Additional Context
------------------

All of the ``Capture*`` functions accept an additional argument for passing a ``map`` of tags
as the second argument. For example:

.. sourcecode:: go

    raven.CaptureError(err, map[string]string{"browser": "Firefox"})

Tags in Sentry help to categories and give you more information about the errors that happened.

Event Sampling
--------------------

To setup client side sampling you can use ``SetSampleRate`` Client function.
Error sampling is disabled by default (sampleRate=1).

.. sourcecode:: go

    package main

    import "github.com/getsentry/raven-go"

    func init() {
        raven.SetSampleRate(0.25)
    }


Deep Dive
---------

For more detailed information about how to get the most out of ``raven-go`` there
is additional documentation available that covers all the rest:

.. toctree::
    :maxdepth: 2
    :titlesonly:

    integrations/index

Resources:

* `Bug Tracker <https://github.com/getsentry/raven-go/issues>`_
* `GitHub Project <https://github.com/getsentry/raven-go>`_
* `Godocs <https://godoc.org/github.com/getsentry/raven-go>`_
