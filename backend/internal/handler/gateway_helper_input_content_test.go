package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWSInputContentSnapshotsAreIsolatedByTurn(t *testing.T) {
	var snapshots openAIWSInputContentSnapshots
	snapshots.Store(1, []byte(`{"input":"first turn"}`))
	snapshots.Store(2, []byte(`{"input":"second turn"}`))

	first := snapshots.Take(1)
	second := snapshots.Take(2)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.Equal(t, "first turn", *first)
	require.Equal(t, "second turn", *second)
	require.Nil(t, snapshots.Take(2))
}
