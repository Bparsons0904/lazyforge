package forge_test

import (
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// noAssets hides OpenAsset: embedding the interface promotes only the Forge methods.
type noAssets struct{ forge.Forge }

func TestCanActAssets(t *testing.T) {
	repo := domain.Repo{RepoRef: domain.RepoRef{Owner: "o", Name: "r"}, Access: domain.AccessRead}
	info := forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h"}

	t.Run("read access and an AssetReader is OK", func(t *testing.T) {
		f := forgetest.NewFake(info)
		if got := forge.Can(f, forge.ActAssets, repo); !got.OK {
			t.Errorf("Can(ActAssets) = %+v, want OK", got)
		}
	})

	t.Run("a forge without AssetReader says it is unsupported", func(t *testing.T) {
		f := noAssets{Forge: forgetest.NewFake(info)}
		got := forge.Can(f, forge.ActAssets, repo)
		if got.OK {
			t.Fatal("Can(ActAssets) is OK for a forge without AssetReader")
		}
		if want := "forgejo doesn't support this"; got.Reason != want {
			t.Errorf("Reason = %q, want %q", got.Reason, want)
		}
	})
}
