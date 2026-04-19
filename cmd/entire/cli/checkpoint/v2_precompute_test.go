package checkpoint

import (
	"context"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/redact"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

// setupV2ForUpdate creates a V2 store and writes an initial committed
// checkpoint so subsequent UpdateCommitted calls have a target.
func setupV2ForUpdate(t *testing.T, initialTranscript []byte) (*git.Repository, *V2GitStore, id.CheckpointID) {
	t.Helper()
	repo := initTestRepo(t)
	store := NewV2GitStore(repo, "origin")
	cpID := id.MustCheckpointID("a1b2c3d4e5f6")

	err := store.WriteCommitted(context.Background(), WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-001",
		Strategy:     "manual-commit",
		Agent:        agent.AgentTypeClaudeCode,
		Transcript:   redact.AlreadyRedacted(initialTranscript),
		Prompts:      []string{"initial prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	})
	require.NoError(t, err)

	return repo, store, cpID
}

// readV2TranscriptBlobHash reads the /full/current transcript blob hash at
// session 0 for the given checkpoint.
func readV2TranscriptBlobHash(t *testing.T, repo *git.Repository, cpID id.CheckpointID) plumbing.Hash {
	t.Helper()
	tree := v2FullTree(t, repo)
	transcriptPath := cpID.Path() + "/0/" + paths.V2RawTranscriptFileName
	file, err := tree.File(transcriptPath)
	require.NoError(t, err, "transcript blob not found at %s", transcriptPath)
	return file.Hash
}

// TestV2UpdateCommitted_PrecomputedBlobs_Roundtrip verifies that passing
// precomputed blob hashes produces the same /full/current transcript content
// as the non-precomputed path.
func TestV2UpdateCommitted_PrecomputedBlobs_Roundtrip(t *testing.T) {
	t.Parallel()
	repo, store, cpID := setupV2ForUpdate(t, []byte(`{"type":"assistant","message":"initial"}`))

	transcript := redact.AlreadyRedacted([]byte(`{"type":"assistant","message":"finalized content"}`))
	precomputed, err := PrecomputeTranscriptBlobs(context.Background(), repo, transcript, agent.AgentTypeClaudeCode)
	require.NoError(t, err)
	require.NotEmpty(t, precomputed.ChunkHashes)
	require.False(t, precomputed.ContentHashBlob.IsZero())

	err = store.UpdateCommitted(context.Background(), UpdateCommittedOptions{
		CheckpointID:     cpID,
		SessionID:        "session-001",
		Transcript:       transcript,
		Agent:            agent.AgentTypeClaudeCode,
		PrecomputedBlobs: precomputed,
	})
	require.NoError(t, err)

	got := v2ReadFile(t, v2FullTree(t, repo), cpID.Path()+"/0/"+paths.V2RawTranscriptFileName)
	require.Equal(t, string(transcript.Bytes()), got)
}

// TestV2UpdateCommitted_ContentHashShortCircuit verifies that a second update
// with identical transcript content preserves the existing transcript blob
// (no rewrite, no new blob hash).
func TestV2UpdateCommitted_ContentHashShortCircuit(t *testing.T) {
	t.Parallel()
	repo, store, cpID := setupV2ForUpdate(t, []byte(`{"type":"assistant","message":"initial"}`))

	transcript := redact.AlreadyRedacted([]byte(`{"type":"assistant","message":"stable content"}`))

	err := store.UpdateCommitted(context.Background(), UpdateCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-001",
		Transcript:   transcript,
		Agent:        agent.AgentTypeClaudeCode,
	})
	require.NoError(t, err)

	blobBefore := readV2TranscriptBlobHash(t, repo, cpID)

	// Second update with identical content — should short-circuit the
	// /full/current transcript rewrite.
	err = store.UpdateCommitted(context.Background(), UpdateCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-001",
		Transcript:   transcript,
		Agent:        agent.AgentTypeClaudeCode,
	})
	require.NoError(t, err)

	blobAfter := readV2TranscriptBlobHash(t, repo, cpID)
	require.Equal(t, blobBefore, blobAfter,
		"short-circuit failed: /full/current transcript blob hash changed")
}

// TestV2UpdateCommitted_ContentChangedRewrites verifies the v2 short-circuit
// does NOT fire when content actually differs, and that the new content is
// persisted on /full/current.
func TestV2UpdateCommitted_ContentChangedRewrites(t *testing.T) {
	t.Parallel()
	repo, store, cpID := setupV2ForUpdate(t, []byte(`{"type":"assistant","message":"initial"}`))

	first := redact.AlreadyRedacted([]byte(`{"type":"assistant","message":"first version"}`))
	second := redact.AlreadyRedacted([]byte(`{"type":"assistant","message":"second version with more content"}`))

	require.NoError(t, store.UpdateCommitted(context.Background(), UpdateCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-001",
		Transcript:   first,
		Agent:        agent.AgentTypeClaudeCode,
	}))
	blobBefore := readV2TranscriptBlobHash(t, repo, cpID)

	require.NoError(t, store.UpdateCommitted(context.Background(), UpdateCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-001",
		Transcript:   second,
		Agent:        agent.AgentTypeClaudeCode,
	}))
	blobAfter := readV2TranscriptBlobHash(t, repo, cpID)

	require.NotEqual(t, blobBefore, blobAfter,
		"expected /full/current transcript blob to change on content update")

	got := v2ReadFile(t, v2FullTree(t, repo), cpID.Path()+"/0/"+paths.V2RawTranscriptFileName)
	require.Equal(t, string(second.Bytes()), got)
}
