package checkpoint

import (
	"context"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTranscript_V2Found(t *testing.T) {
	t.Parallel()
	repo := initTestRepo(t)
	store := NewV2GitStore(repo)
	cpID := id.MustCheckpointID("a1a2a3a4a5a6")
	ctx := context.Background()

	err := store.WriteCommitted(ctx, WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-1",
		Strategy:     "manual-commit",
		Transcript:   []byte(`{"v2": true}`),
		Prompts:      []string{"prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	})
	require.NoError(t, err)

	content, err := ResolveTranscript(ctx, repo, cpID, 0, true)
	require.NoError(t, err)
	require.NotNil(t, content)
	assert.NotEmpty(t, content.Transcript)
	assert.Equal(t, "session-1", content.Metadata.SessionID)
}

func TestResolveTranscript_V2Disabled_FallsBackToV1(t *testing.T) {
	t.Parallel()
	repo := initTestRepo(t)
	cpID := id.MustCheckpointID("b1b2b3b4b5b6")
	ctx := context.Background()

	v1Store := NewGitStore(repo)
	err := v1Store.WriteCommitted(ctx, WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "v1-session",
		Strategy:     "manual-commit",
		Transcript:   []byte(`{"v1": true}`),
		Prompts:      []string{"v1 prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	})
	require.NoError(t, err)

	content, err := ResolveTranscript(ctx, repo, cpID, 0, false)
	require.NoError(t, err)
	require.NotNil(t, content)
	assert.NotEmpty(t, content.Transcript)
	assert.Equal(t, "v1-session", content.Metadata.SessionID)
}

func TestResolveTranscript_V2MissTranscript_FallsBackToV1(t *testing.T) {
	t.Parallel()
	repo := initTestRepo(t)
	cpID := id.MustCheckpointID("c1c2c3c4c5c6")
	ctx := context.Background()

	v2Store := NewV2GitStore(repo)
	err := v2Store.WriteCommitted(ctx, WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "v2-session",
		Strategy:     "manual-commit",
		Prompts:      []string{"v2 prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	})
	require.NoError(t, err)

	v1Store := NewGitStore(repo)
	err = v1Store.WriteCommitted(ctx, WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "v1-session",
		Strategy:     "manual-commit",
		Transcript:   []byte(`{"v1": true}`),
		Prompts:      []string{"v1 prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	})
	require.NoError(t, err)

	content, err := ResolveTranscript(ctx, repo, cpID, 0, true)
	require.NoError(t, err)
	require.NotNil(t, content)
	assert.NotEmpty(t, content.Transcript)
}
