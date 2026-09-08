package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngine_AnonymousRequestDoesNotPersistSession is the audit H-06 guard:
// a request with no session id (every SDK's default) runs on a throwaway
// session, so a load test cannot pin one 30-minute session per request.
// Pinned ids keep persisting exactly as before.
func TestEngine_AnonymousRequestDoesNotPersistSession(t *testing.T) {
	eng := newTestEngine(fullAgent("a", "gpt-4o"))

	for i := 0; i < 5; i++ {
		resp, err := eng.ProcessRequest(&InboundRequest{
			AgentName: "a",
			Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "Hi there!", resp.Content, "anonymous turns still match scenarios")
	}
	assert.Equal(t, 0, eng.States.Count(), "anonymous requests must not be retained")

	// A pinned session is retained and accumulates turns.
	for i := 0; i < 2; i++ {
		_, err := eng.ProcessRequest(&InboundRequest{
			AgentName: "a",
			SessionID: "pinned",
			Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 1, eng.States.Count())
	assert.Equal(t, 2, eng.States.Get(scopedSessionKey("", "a", "pinned")).TurnCount)
}
