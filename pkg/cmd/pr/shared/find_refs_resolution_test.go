package shared

import (
	"errors"
	"testing"

	ghContext "github.com/cli/cli/v2/context"
	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/internal/ghrepo"
	o "github.com/cli/cli/v2/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQualifiedHeadRef(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		behavior           string
		ref                string
		expectedString     string
		expectedBranchName string
		expectedError      error
	}{
		{
			behavior:           "when a branch is provided, the parsed qualified head ref only has a branch",
			ref:                "feature-branch",
			expectedString:     "feature-branch",
			expectedBranchName: "feature-branch",
		},
		{
			behavior:           "when an owner and branch are provided, the parsed qualified head ref has both",
			ref:                "owner:feature-branch",
			expectedString:     "owner:feature-branch",
			expectedBranchName: "feature-branch",
		},
		{
			behavior:      "when the structure cannot be interpreted correctly, an error is returned",
			ref:           "owner:feature-branch:extra",
			expectedError: errors.New("invalid qualified head ref format 'owner:feature-branch:extra'"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.behavior, func(t *testing.T) {
			t.Parallel()

			qualifiedHeadRef, err := ParseQualifiedHeadRef(tc.ref)
			if tc.expectedError != nil {
				require.Equal(t, tc.expectedError, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedString, qualifiedHeadRef.String())
			assert.Equal(t, tc.expectedBranchName, qualifiedHeadRef.BranchName())
		})
	}
}

func TestPRFindRefs(t *testing.T) {
	t.Parallel()

	t.Run("qualified head ref with owner", func(t *testing.T) {
		t.Parallel()

		refs := PRFindRefs{
			qualifiedHeadRef: mustParseQualifiedHeadRef("forkowner:feature-branch"),
		}

		require.Equal(t, "forkowner:feature-branch", refs.QualifiedHeadRef())
		require.Equal(t, "feature-branch", refs.UnqualifiedHeadRef())
	})

	t.Run("qualified head ref without owner", func(t *testing.T) {
		t.Parallel()

		refs := PRFindRefs{
			qualifiedHeadRef: mustParseQualifiedHeadRef("feature-branch"),
		}

		require.Equal(t, "feature-branch", refs.QualifiedHeadRef())
		require.Equal(t, "feature-branch", refs.UnqualifiedHeadRef())
	})

	t.Run("base repo", func(t *testing.T) {
		t.Parallel()

		refs := PRFindRefs{
			baseRepo: ghrepo.New("owner", "repo"),
		}

		require.True(t, ghrepo.IsSame(refs.BaseRepo(), ghrepo.New("owner", "repo")), "expected repos to be the same")
	})

	t.Run("matches", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			behaviour        string
			refs             PRFindRefs
			baseBranchName   string
			qualifiedHeadRef string
			expectedMatch    bool
		}{
			{
				behaviour: "when qualified head refs don't match, returns false",
				refs: PRFindRefs{
					qualifiedHeadRef: mustParseQualifiedHeadRef("owner:feature-branch"),
				},
				baseBranchName:   "feature-branch",
				qualifiedHeadRef: "feature-branch",
				expectedMatch:    false,
			},
			{
				behaviour: "when base branches don't match, returns false",
				refs: PRFindRefs{
					qualifiedHeadRef: mustParseQualifiedHeadRef("feature-branch"),
					baseBranchName:   o.Some("not-main"),
				},
				baseBranchName:   "main",
				qualifiedHeadRef: "feature-branch",
				expectedMatch:    false,
			},
			{
				behaviour: "when head refs match and there is no base branch, returns true",
				refs: PRFindRefs{
					qualifiedHeadRef: mustParseQualifiedHeadRef("feature-branch"),
					baseBranchName:   o.None[string](),
				},
				baseBranchName:   "main",
				qualifiedHeadRef: "feature-branch",
				expectedMatch:    true,
			},
			{
				behaviour: "when head refs match and base branches match, returns true",
				refs: PRFindRefs{
					qualifiedHeadRef: mustParseQualifiedHeadRef("feature-branch"),
					baseBranchName:   o.Some("main"),
				},
				baseBranchName:   "main",
				qualifiedHeadRef: "feature-branch",
				expectedMatch:    true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.behaviour, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, tc.expectedMatch, tc.refs.Matches(tc.baseBranchName, tc.qualifiedHeadRef))
			})
		}
	})
}

func TestPullRequestResolution(t *testing.T) {
	t.Parallel()

	baseRepo := ghrepo.New("owner", "repo")
	baseRemote := ghContext.Remote{
		Remote: &git.Remote{
			Name: "upstream",
		},
		Repo: ghrepo.New("owner", "repo"),
	}

	forkRemote := ghContext.Remote{
		Remote: &git.Remote{
			Name: "origin",
		},
		Repo: ghrepo.New("otherowner", "repo-fork"),
	}

	t.Run("when the base repo is nil, returns an error", func(t *testing.T) {
		t.Parallel()

		resolver := NewPullRequestFindRefsResolver(stubGitConfigClient{}, dummyRemotesFn)
		_, err := resolver.ResolvePullRequestRefs(nil, "", "")
		require.Error(t, err)
	})

	t.Run("when the local branch name is empty, returns an error", func(t *testing.T) {
		t.Parallel()

		resolver := NewPullRequestFindRefsResolver(stubGitConfigClient{}, dummyRemotesFn)
		_, err := resolver.ResolvePullRequestRefs(baseRepo, "", "")
		require.Error(t, err)
	})

	t.Run("when the default pr head has a repo, it is used for the refs", func(t *testing.T) {
		t.Parallel()

		resolver := NewPullRequestFindRefsResolver(
			stubGitConfigClient{
				pushRevisionFn: stubPushRevision(git.RemoteTrackingRef{
					Remote: "origin",
					Branch: "feature-branch",
				}, nil),
			},
			stubRemotes(ghContext.Remotes{&baseRemote, &forkRemote}, nil),
		)

		refs, err := resolver.ResolvePullRequestRefs(baseRepo, "main", "feature-branch")
		require.NoError(t, err)

		expectedRefs := PRFindRefs{
			qualifiedHeadRef: QualifiedHeadRef{
				owner:      o.Some("otherowner"),
				branchName: "feature-branch",
			},
			baseRepo:       baseRepo,
			baseBranchName: o.Some("main"),
		}

		require.Equal(t, expectedRefs, refs)
	})

	t.Run("when the default pr head does not have a repo, we use the base repo for the head", func(t *testing.T) {
		t.Parallel()

		// All the values stubbed here result in being unable to resolve a default repo.
		noRepoResolutionStubClient := stubGitConfigClient{
			pushRevisionFn:      stubPushRevision(git.RemoteTrackingRef{}, errors.New("test error")),
			readBranchConfigFn:  stubBranchConfig(git.BranchConfig{}, nil),
			pushDefaultFn:       stubPushDefault("", nil),
			remotePushDefaultFn: stubRemotePushDefault("", nil),
		}

		resolver := NewPullRequestFindRefsResolver(
			noRepoResolutionStubClient,
			stubRemotes(ghContext.Remotes{&baseRemote, &forkRemote}, nil),
		)

		refs, err := resolver.ResolvePullRequestRefs(baseRepo, "main", "feature-branch")
		require.NoError(t, err)

		expectedRefs := PRFindRefs{
			qualifiedHeadRef: QualifiedHeadRef{
				owner:      o.None[string](),
				branchName: "feature-branch",
			},
			baseRepo:       baseRepo,
			baseBranchName: o.Some("main"),
		}
		require.Equal(t, expectedRefs, refs)
	})
}

func dummyRemotesFn() (ghContext.Remotes, error) {
	panic("not implemented")
}

func mustParseQualifiedHeadRef(ref string) QualifiedHeadRef {
	parsed, err := ParseQualifiedHeadRef(ref)
	if err != nil {
		panic(err)
	}
	return parsed
}
