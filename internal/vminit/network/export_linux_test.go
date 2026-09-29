package network //nolint:testpackage // Exposes the messages to the tests of the package.

// The messages Configure sends, and the reading of an acknowledgement.
var (
	AddrMessage   = addrMessage
	LinkUpMessage = linkUpMessage
	RouteMessage  = routeMessage
	AckError      = ackError
)
