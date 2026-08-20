package store

const (
	statCreated    = "created"
	statBurned     = "burned"
	statExpired    = "expired"
	statPassphrase = "passphrase"
)

// Counters are durable monotonic totals. They survive burn, expire, and pod rolls.
// No ids, IPs, or ciphertext.
type Counters struct {
	Created    int64 `json:"created"`
	Burned     int64 `json:"burned"`
	Expired    int64 `json:"expired"`
	Passphrase int64 `json:"passphrase"`
}

func countersFromMap(m map[string]int64) Counters {
	return Counters{
		Created:    m[statCreated],
		Burned:     m[statBurned],
		Expired:    m[statExpired],
		Passphrase: m[statPassphrase],
	}
}
