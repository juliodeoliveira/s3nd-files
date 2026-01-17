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
	loadOnlyFolders := func(bucket, prefix string, count int) {
		items, _, err := s3Client.ListObjectsPaginated(context.Background(), bucket, prefix, 100)
		if err != nil {
			runOnUIThread(func() {
				dialog.ShowError(fmt.Errorf("falha ao listar pastas: %v", err), w)
			})
			return
		}
		
		// Filtrar apenas pastas
		var folders []s3.Item
		for _, item := range items {
			if item.Type == s3.Folder {
				folders = append(folders, item)
			}
		}
		
		// Adicionar ".." se não estiver na raiz
		if prefix != "" {
			folders = append([]s3.Item{{Name: "..", Type: s3.Folder}}, folders...)
		}
		
		runOnUIThread(func() {
			s3Items = folders
			currentBucket = bucket
			currentPrefix = prefix
			s3Status.SetText(fmt.Sprintf("Bucket: %s | Pasta: %s (%d+ itens, mostrando %d pastas)", 
				bucket, prefix, count, len(folders)))
			s3List.Refresh()
		})
	}

	loadFirstItems := func(bucket, prefix string, limit int32, total int) {
		items, nextToken, err := s3Client.ListObjectsPaginated(context.Background(), bucket, prefix, limit)
		if err != nil {
			runOnUIThread(func() {
				dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
			})
			return
		}
		
		// Adicionar ".." se não estiver na raiz
		if prefix != "" {
			items = append([]s3.Item{{Name: "..", Type: s3.Folder}}, items...)
		}
		
		// Adicionar item para carregar mais se houver
		if nextToken != "" {
			items = append(items, s3.Item{
				Name: fmt.Sprintf("... (carregar mais, %d+ itens restantes)", total-len(items)+1),
				Type: s3.Folder,
				Prefix: "LOAD_MORE",
			})
		}
		
		runOnUIThread(func() {
			s3Items = items
			currentBucket = bucket
			currentPrefix = prefix
			s3Status.SetText(fmt.Sprintf("Bucket: %s | Pasta: %s (mostrando %d de %d+ itens)", 
				bucket, prefix, len(items), total))
			s3List.Refresh()
		})
	}
	
	// Função para navegar com tratamento de pastas grandes
	// ui/window.go - Versão corrigida usando runOnUIThread

	navigateWithLimit := func(bucket, prefix string) {
		if !s3Connected || s3Client == nil {
			return
		}
		
		// Mostrar loading - usar runOnUIThread
		runOnUIThread(func() {
			loadingDialog := dialog.NewProgressInfinite("Analisando", 
				fmt.Sprintf("Verificando conteúdo..."), w)
			loadingDialog.Show()
			
			go func() {
				defer func() {
					// Esconder loading - usar runOnUIThread
					runOnUIThread(func() {
						loadingDialog.Hide()
					})
				}()
				
				// Primeiro contar objetos (operação de rede, em goroutine OK)
				count, err := s3Client.CountObjects(context.Background(), bucket, prefix)
				if err != nil {
					fmt.Printf("Erro ao contar objetos: %v\n", err)
					count = 0
				}
				
				fmt.Printf("DEBUG: Pasta %s/%s tem aproximadamente %d itens\n", bucket, prefix, count)
				
				// Se for uma pasta com muitos arquivos
				if count > 1000 {
					// Fechar loading primeiro
					runOnUIThread(func() {
						loadingDialog.Hide()
					})
					
					// Mostrar diálogo de opções - usar runOnUIThread
					runOnUIThread(func() {
						dialog.ShowCustom(
							"Muitos Itens",
							"Cancelar",
							container.NewVBox(
								widget.NewLabel(fmt.Sprintf("Esta pasta contém aproximadamente %d itens.", count)),
								widget.NewLabel("Para melhor performance:"),
								widget.NewButton("📁 Ver apenas pastas (recomendado)", func() {
									go func() {
										loadOnlyFolders(bucket, prefix, count)
									}()
								}),
								widget.NewButton("⬇ Carregar primeiros 500 itens", func() {
									go func() {
										loadFirstItems(bucket, prefix, 500, count)
									}()
								}),
							),
							w,
						)
					})
					return
				}
				
				// Para pastas pequenas, carregar normalmente
				items, err := s3Client.ListObjects(context.Background(), bucket, prefix)
				if err != nil {
					runOnUIThread(func() {
						dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
					})
					return
				}
				
				// Adicionar ".." se não estiver na raiz
				if prefix != "" {
					items = append([]s3.Item{{Name: "..", Type: s3.Folder}}, items...)
				}
				
				// Atualizar UI - usar runOnUIThread
				runOnUIThread(func() {
					s3Items = items
					currentBucket = bucket
					currentPrefix = prefix
					
					statusText := fmt.Sprintf("Bucket: %s | Pasta: %s", bucket, prefix)
					if count > 0 {
						statusText += fmt.Sprintf(" (%d itens)", len(items))
					}
					s3Status.SetText(statusText)
					s3List.Refresh()
				})
			}()
		})
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
			// Entrar no bucket
			// currentBucket = item.Name
			// currentPrefix = ""
			navigateWithLimit(item.Name, "")

			// Mostrar loading
			loadingDialog := dialog.NewProgressInfinite("Carregando", 
				fmt.Sprintf("Listando objetos do bucket %s...", currentBucket), w)
			loadingDialog.Show()
			
			go func() {
				defer loadingDialog.Hide()
				
				// Listar objetos reais do bucket
				items, err := s3Client.ListObjects(context.Background(), currentBucket, currentPrefix)
				if err != nil {
					dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
					return
				}
				
				// Atualizar lista
				s3Items = items
				s3Status.SetText(fmt.Sprintf("Bucket: %s", currentBucket))
				s3List.Refresh()
			}()
			
		} else if item.Type == s3.Folder {
			loadingDialog := dialog.NewProgressInfinite("Navegando", 
				fmt.Sprintf("Entrando na pasta %s...", item.Name), w)
			loadingDialog.Show()
			
			go func() {
				defer loadingDialog.Hide()
				
				if item.Name == ".." {
					// Voltar para o pai
					if currentPrefix == "" {
						// Voltar para lista de buckets
						currentBucket = ""
						
						// Listar buckets novamente
						buckets, err := s3Client.ListBuckets(context.Background())
						if err != nil {
							dialog.ShowError(err, w)
							return
						}
						
						s3Items = make([]s3.Item, 0, len(buckets))
						for _, bucketName := range buckets {
							s3Items = append(s3Items, s3.Item{
								Name: bucketName,
								Type: s3.Bucket,
							})
						}
						s3Status.SetText(fmt.Sprintf("Conectado - %d bucket(s)", len(buckets)))
					} else {
						// Subir um nível no prefixo
						currentPrefix = getParentPrefix(currentPrefix)
						
						// Listar objetos no novo prefixo
						items, err := s3Client.ListObjects(context.Background(), currentBucket, currentPrefix)
						if err != nil {
							dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
							return
						}
						
						s3Items = items
						// Adicionar ".." se não estiver na raiz
						if currentPrefix != "" {
							s3Items = append([]s3.Item{{Name: "..", Type: s3.Folder}}, s3Items...)
						}
					}
				} else {
					// Entrar na pasta
					currentPrefix = item.Prefix
					
					// Listar objetos na pasta
					items, err := s3Client.ListObjects(context.Background(), currentBucket, currentPrefix)
					if err != nil {
						dialog.ShowError(fmt.Errorf("falha ao listar objetos: %v", err), w)
						return
					}
					
					s3Items = items
					// Sempre adicionar ".." para voltar
					s3Items = append([]s3.Item{{Name: "..", Type: s3.Folder}}, s3Items...)
				}
				
				// Atualizar UI
				s3Status.SetText(fmt.Sprintf("Bucket: %s | Pasta: %s", currentBucket, currentPrefix))
				s3List.Refresh()
			}()
			
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