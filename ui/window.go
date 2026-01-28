// ui/window.go
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"s3nd-files/s3"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Remova as definições antigas de S3Item e S3ItemType
// e use as do pacote s3

func Run() {
	a := app.New()
	w := a.NewWindow("S3 Uploader")
	w.Resize(fyne.NewSize(900, 500))


	runOnUIThread := func(f func()) {
		// Executa diretamente (funciona para muitas operações do Fyne)
		// Se houver problemas, troque por fyne.CurrentApp().Run()
		f()
	}
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
	// S3 - variáveis
	// =====================
	var (
		s3Client    *s3.Client
		s3Items     []s3.Item
		s3Connected bool
	)

	// Variáveis para navegação
	currentBucket := ""
	currentPrefix := ""

	s3Header := widget.NewLabelWithStyle("Arquivos na S3", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	s3Status := widget.NewLabel("Não conectado")

	s3List := widget.NewList(
		func() int { return len(s3Items) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(s3Items) {
				return
			}
			item := s3Items[id]

			icon := "📄 "
			if item.Type == s3.Bucket {
				icon = "🪣 "
			} else if item.Type == s3.Folder {
				icon = "📁 "
			}

			obj.(*widget.Label).SetText(icon + item.Name)
		},
	)

	// Função auxiliar para obter prefixo pai
	getParentPrefix := func(prefix string) string {
		if prefix == "" {
			return ""
		}
		parts := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
		if len(parts) <= 1 {
			return ""
		}
		return strings.Join(parts[:len(parts)-1], "/") + "/"
	}

	// Adicione esta função APÓS getParentPrefix e ANTES de "Container inicial da S3"
// Funções auxiliares
	// loadOnlyFolders := func(bucket, prefix string, count int) {
	// 	items, _, err := s3Client.ListObjectsPaginated(context.Background(), bucket, prefix, 100)
	// 	if err != nil {
	// 		runOnUIThread(func() {
	// 			dialog.ShowError(fmt.Errorf("falha ao listar pastas: %v", err), w)
	// 		})
	// 		return
	// 	}
		
	// 	// Filtrar apenas pastas
	// 	var folders []s3.Item
	// 	for _, item := range items {
	// 		if item.Type == s3.Folder {
	// 			folders = append(folders, item)
	// 		}
	// 	}
		
	// 	// Adicionar ".." se não estiver na raiz
	// 	if prefix != "" {
	// 		folders = append([]s3.Item{{Name: "..", Type: s3.Folder}}, folders...)
	// 	}
		
	// 	runOnUIThread(func() {
	// 		s3Items = folders
	// 		currentBucket = bucket
	// 		currentPrefix = prefix
	// 		s3Status.SetText(fmt.Sprintf("Bucket: %s | Pasta: %s (%d+ itens, mostrando %d pastas)", 
	// 			bucket, prefix, count, len(folders)))
	// 		s3List.Refresh()
	// 	})
	// }

	// loadFirstItems := func(bucket, prefix string, limit int32, total int) {
	// 	items, nextToken, err := s3Client.ListObjectsPaginated(context.Background(), bucket, prefix, limit)
	// 	if err != nil {
	// 		runOnUIThread(func() {
	// 			dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
	// 		})
	// 		return
	// 	}
		
	// 	// Adicionar ".." se não estiver na raiz
	// 	if prefix != "" {
	// 		items = append([]s3.Item{{Name: "..", Type: s3.Folder}}, items...)
	// 	}
		
	// 	// Adicionar item para carregar mais se houver
	// 	if nextToken != "" {
	// 		items = append(items, s3.Item{
	// 			Name: fmt.Sprintf("... (carregar mais, %d+ itens restantes)", total-len(items)+1),
	// 			Type: s3.Folder,
	// 			Prefix: "LOAD_MORE",
	// 		})
	// 	}
		
	// 	runOnUIThread(func() {
	// 		s3Items = items
	// 		currentBucket = bucket
	// 		currentPrefix = prefix
	// 		s3Status.SetText(fmt.Sprintf("Bucket: %s | Pasta: %s (mostrando %d de %d+ itens)", 
	// 			bucket, prefix, len(items), total))
	// 		s3List.Refresh()
	// 	})
	// }
	
	// Função para navegar com tratamento de pastas grandes
	// ui/window.go - Versão corrigida usando runOnUIThread

	// ui/window.go - Função navigateWithLimit corrigida

	// ui/window.go - Remova estas funções completamente:

// REMOVA estas funções:
// loadOnlyFolders
// loadFirstItems

// Simplifique a função navigateWithLimit:

	navigateWithLimit := func(bucket, prefix string) {
		if !s3Connected || s3Client == nil {
			return
		}
		
		// Criar dialog na thread principal
		loadingDialog := dialog.NewProgressInfinite("Carregando", 
			fmt.Sprintf("Listando %s/%s...", bucket, prefix), w)
		loadingDialog.Show()
		
		go func() {
			// Esconder dialog no final
			defer runOnUIThread(func() {
				loadingDialog.Hide()
			})
			
			// Remova toda a lógica de contagem e diálogo de "Muitos Itens"
			// Apenas liste diretamente
			items, err := s3Client.ListObjects(context.Background(), bucket, prefix)
			if err != nil {
				runOnUIThread(func() {
					dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
				})
				return
			}
			
			// Adicionar ".." para navegação
			if bucket != "" {
				items = append([]s3.Item{{Name: "..", Type: s3.Folder}}, items...)
			}
			
			// Atualizar UI
			runOnUIThread(func() {
				s3Items = items
				currentBucket = bucket
				currentPrefix = prefix
				
				statusText := fmt.Sprintf("Bucket: %s", bucket)
				if prefix != "" {
					statusText += fmt.Sprintf(" | Pasta: %s", prefix)
				}
				statusText += fmt.Sprintf(" (%d itens)", len(items))
				s3Status.SetText(statusText)
				s3List.Refresh()
			})
		}()
	}

// Funções auxiliares - também precisam usar runOnUIThread para atualizações de UI

	// Container inicial da S3
	initialS3Content := container.NewCenter(
		container.NewVBox(s3Status, widget.NewButton("Conectar à S3", nil)),
	)

	// Criar um container que podemos atualizar
	s3Container := container.NewStack(initialS3Content)

	s3Panel := container.NewBorder(
		s3Header,
		nil, nil, nil,
		s3Container,
	)

	// Botão de conectar
	connectBtn := widget.NewButton("Conectar à S3", func() {
		fmt.Println("Conectando ao MinIO...")
		
		go func() {
			client, err := s3.New(s3.Config{
				Endpoint:  "http://192.168.3.6:9000",
				Region:    "us-east-1",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
				UseSSL:    false,
			})
			
			if err != nil {
				runOnUIThread(func() {
					dialog.ShowError(fmt.Errorf("falha ao criar cliente S3: %v", err), w)
				})
				return
			}
			
			fmt.Println("Cliente S3 criado, listando buckets...")
			
			// Testar conexão
			buckets, err := client.ListBuckets(context.Background())
			if err != nil {
				runOnUIThread(func() {
					dialog.ShowError(fmt.Errorf("falha ao listar buckets: %v", err), w)
				})
				return
			}
			
			fmt.Printf("Sucesso! %d bucket(s) encontrado(s)\n", len(buckets))
			
			// Atualizar estado
			s3Client = client
			s3Connected = true
			
			// Atualizar lista de itens na thread principal
			runOnUIThread(func() {
				s3Items = make([]s3.Item, 0, len(buckets))
				for _, bucketName := range buckets {
					s3Items = append(s3Items, s3.Item{
						Name: bucketName,
						Type: s3.Bucket,
					})
				}
				
				// Atualizar UI
				s3Status.SetText(fmt.Sprintf("Conectado - %d bucket(s)", len(buckets)))
				
				if len(s3Items) > 0 {
					s3Container.Objects = []fyne.CanvasObject{s3List}
				} else {
					s3Container.Objects = []fyne.CanvasObject{container.NewCenter(
						widget.NewLabel("Nenhum bucket encontrado"),
					)}
				}
				
				s3List.Refresh()
				s3Container.Refresh()
			})
		}()
	})

	// Atualizar o conteúdo inicial
	initialS3Content = container.NewCenter(
		container.NewVBox(s3Status, connectBtn),
	)
	s3Container.Objects = []fyne.CanvasObject{initialS3Content}

	// Configurar ação ao selecionar item na lista S3
	s3List.OnSelected = func(id widget.ListItemID) {
		if !s3Connected || s3Client == nil || id < 0 || id >= len(s3Items) {
			return
		}

		item := s3Items[id]
		
		if item.Type == s3.Bucket {
			// Usar a nova função de navegação com limite
			navigateWithLimit(item.Name, "")
			
		} else if item.Type == s3.Folder {
			if item.Name == ".." {
				// Lógica para voltar...
				if currentPrefix == "" {
					// Voltar para lista de buckets
					navigateWithLimit("", "")
				} else {
					// Subir um nível
					parentPrefix := getParentPrefix(currentPrefix)
					navigateWithLimit(currentBucket, parentPrefix)
				}
			} else {
				// Entrar na pasta
				navigateWithLimit(currentBucket, item.Prefix)
			}
			
		} else if item.Type == s3.File {
			// Mostrar informações do arquivo
			fileInfo := fmt.Sprintf("Arquivo: %s\nBucket: %s\nCaminho: %s", 
				item.Name, currentBucket, item.Prefix)
			
			dialog.ShowInformation("Informações do Arquivo", fileInfo, w)
		}
	}

	// =====================
	// Botão de Upload simplificado
	// =====================
	uploadBtn := widget.NewButton("📤 Upload", func() {
		if !s3Connected || s3Client == nil {
			dialog.ShowInformation("Não conectado", 
				"Conecte-se à S3 primeiro", w)
			return
		}

		if len(files) == 0 {
			dialog.ShowInformation("Nenhum arquivo", 
				"Selecione arquivos locais primeiro", w)
			return
		}

		if currentBucket == "" {
			dialog.ShowInformation("Selecione bucket", 
				"Selecione um bucket na S3 para upload", w)
			return
		}

		// Diálogo de confirmação
		dialog.ShowConfirm("Confirmar Upload", 
			fmt.Sprintf("Deseja fazer upload de %d arquivo(s) para:\n\nBucket: %s\nPasta: %s", 
				len(files), currentBucket, currentPrefix),
			func(confirm bool) {
				if !confirm {
					return
				}
				
				// Diálogo de progresso simples
				progressDialog := dialog.NewProgress("Upload em andamento", 
					fmt.Sprintf("Enviando %d arquivos...", len(files)), w)
				progressDialog.Show()
				
				go func() {
					successCount := 0
					
					for i, filePath := range files {
						// Calcular progresso
						progress := float64(i) / float64(len(files))
						progressDialog.SetValue(progress)
						
						// Criar chave (key) para o S3
						key := currentPrefix + filepath.Base(filePath)
						
						// Fazer upload (implemente este método no cliente S3)
						fmt.Printf("Uploading %s to %s/%s\n", filePath, currentBucket, key)
						err := s3Client.UploadFile(context.Background(), currentBucket, key, filePath)
						if err != nil {
						    fmt.Printf("Erro: %v\n", err)
						} else {
						    successCount++
						}
						
						// Simular upload por enquanto
						successCount++
					}
					
					// Fechar diálogo de progresso
					progressDialog.Hide()
					
					// Mostrar resultado
					message := fmt.Sprintf("Upload simulado!\n\nArquivos processados: %d", 
						successCount)
					dialog.ShowInformation("Resultado", message, w)
				}()
			}, w)
	})

	// Adicionar botão de upload ao header local
	localHeaderWithUpload := container.NewHBox(
		widget.NewLabelWithStyle(
			"Arquivos locais",
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		),
		container.NewBorder(nil, nil, nil, uploadBtn),
	)

	localPanel = container.NewBorder(
		container.NewVBox(
			localHeaderWithUpload,
			container.NewHBox(selectFolderBtn, selectFileBtn, clearBtn),
		),
		nil,
		nil,
		nil,
		localList,
	)

	// =====================
	// Layout final
	// =====================
	content := container.NewHSplit(localPanel, s3Panel)
	content.SetOffset(0.55)

	w.SetContent(content)
	w.ShowAndRun()
}