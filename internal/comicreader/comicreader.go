// Package comicreader presents manga pages in an Android RecyclerView.
package comicreader

import (
	"context"
	"io"

	"github.com/egoist/mygo/native"
)

// PageLoader fetches an image through the app's existing source transport.
type PageLoader interface {
	Load(ctx context.Context, address string) ([]byte, error)
}

// Reader owns the Android reading view. Closing a chapter stops its loads.
//
//mygo:native
type Reader interface {
	native.View
	Open(pages []string, firstPage int, hasNext bool, dark bool, loader PageLoader,
		onPage func(int), onTap func(), onEnd func()) io.Closer
	Seek(page int)
	SetDark(dark bool)
}
