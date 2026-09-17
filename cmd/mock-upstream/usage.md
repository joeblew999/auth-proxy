### Serving

The mock answers the two paths the proxy calls and refuses everything else, so
a test can point the proxy at it and assert on what comes back without a real
provider or a real key.
