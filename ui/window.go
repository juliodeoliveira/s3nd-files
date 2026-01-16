package ui

import (
	"os"
	"path/filepath"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func Run() {
	a := app.New()
	w := a.NewWindow("S3 Uploader")

	w.Resize(fyne.NewSize(900, 500))

	// =====================
	// Arquivos locais
	// =====================
	fileSet := make(map[string]struct{})
	var files []string

	localList := widget.NewList(
		func() int {
			return len(files)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(files[id])
		},
	)

	refreshList := func() {
		files = files[:0]
		for path := range fileSet {
			files = append(files, path)
		}
		sort.Strings(files)
		localList.Refresh()
	}

	selectFolderBtn := widget.NewButton("Selecionar pasta", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}

			root := uri.Path()

			filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() {
					fileSet[path] = struct{}{}
				}
				return nil
			})

			refreshList()
		}, w)
	})

	selectFileBtn := widget.NewButton("Selecionar arquivo", func() {
		dialog.ShowFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			fileSet[r.URI().Path()] = struct{}{}
			refreshList()
		}, w)
	})

	clearBtn := widget.NewButton("Limpar seleção", func() {
		fileSet = make(map[string]struct{})
		files = files[:0]
		localList.Refresh()
	})

	localHeader := widget.NewLabelWithStyle(
		"Arquivos locais",
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	localPanel := container.NewBorder(
		container.NewVBox(
			localHeader,
			container.NewHBox(selectFolderBtn, selectFileBtn, clearBtn),
		),
		nil,
		nil,
		nil,
		localList,
	)

	// =====================
	// Arquivos S3 (placeholder)
	// =====================
	s3Header := widget.NewLabelWithStyle(
		"Arquivos na S3",
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	s3Status := widget.NewLabel("Não conectado")

	s3Panel := container.NewBorder(
		s3Header,
		nil,
		nil,
		nil,
		container.NewCenter(s3Status),
	)

	// =====================
	// Layout final
	// =====================
	content := container.NewHSplit(localPanel, s3Panel)
	content.SetOffset(0.55)

	w.SetContent(content)
	w.ShowAndRun()
}
