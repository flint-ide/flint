package main

import "flint/internal/preview"

// PreviewAPI renders Markdown previews and reads images for the image tab.
type PreviewAPI struct {
	app *App
}

// Markdown renders Markdown source to safe HTML (no raw HTML, no script links);
// remote images wait for the user (data-remote-src).
func (p *PreviewAPI) Markdown(src string) (string, error) {
	return preview.MarkdownBlockingRemote(src)
}

// Image reads an image inside the open folders as a data URL (capped in size).
func (p *PreviewAPI) Image(path string) (preview.Image, error) {
	return preview.ReadImage(path, p.app.projectRoots())
}

// OpenedImage reads an image the user opened in a tab as a data URL, wherever
// it is (a screenshot an agent left in a temp folder), as the editor reads any
// file the user opens. Images named by a document go through Image.
func (p *PreviewAPI) OpenedImage(path string) (preview.Image, error) {
	return preview.ReadOpenedImage(path)
}
