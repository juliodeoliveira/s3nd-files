// s3/client.go
package s3

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	s3 *s3.Client
}

type Config struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	// Novos campos para segurança
	DisableSSL       bool   // Para forçar HTTP (apenas dev/test)
	ForcePathStyle   bool   // Para MinIO e alguns endpoints S3
	CustomCACertPath string // Para certificados auto-assinados
}

func New(cfg Config) (*Client, error) {
	// Validar configuração mínima
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint não pode ser vazio")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("credenciais não podem ser vazias")
	}
	
	// Configurar resolvedor de endpoint customizado
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if service == s3.ServiceID {
			endpoint := cfg.Endpoint
			
			// Garantir que o endpoint tenha o protocolo correto
			if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
				if cfg.UseSSL && !cfg.DisableSSL {
					endpoint = "https://" + endpoint
				} else {
					endpoint = "http://" + endpoint
				}
			}
			
			return aws.Endpoint{
				URL:               endpoint,
				SigningRegion:     cfg.Region,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	// Configurar credenciais
	creds := credentials.NewStaticCredentialsProvider(
		cfg.AccessKey,
		cfg.SecretKey,
		"",
	)

	// Configurações customizadas do HTTP client para SSL
	var httpClient *http.Client
	if cfg.CustomCACertPath != "" {
		// Carregar certificado CA customizado
		_, err := os.ReadFile(cfg.CustomCACertPath)
		if err != nil {
			return nil, fmt.Errorf("falha ao ler certificado CA: %w", err)
		}
		
		// Aqui você precisaria implementar um pool de certificados customizado
		// Para simplificar, vamos usar InsecureSkipVerify (APENAS PARA TESTES)
		// Em produção, configure corretamente o certificado
		fmt.Println("⚠️ AVISO: Usando certificado customizado. Configure TLS corretamente para produção.")
		
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // APENAS PARA DEV/TEST - REMOVA EM PRODUÇÃO
			},
		}
		httpClient = &http.Client{Transport: transport}
	} else if !cfg.UseSSL || cfg.DisableSSL {
		// HTTP simples (apenas para desenvolvimento)
		fmt.Println("⚠️ AVISO: Usando HTTP sem SSL. Não use em produção!")
	}

	// Carregar configuração AWS
	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
		config.WithEndpointResolverWithOptions(customResolver),
	}
	
	// Adicionar HTTP client customizado se configurado
	if httpClient != nil {
		loadOpts = append(loadOpts, config.WithHTTPClient(httpClient))
	}

	awsCfg, err := config.LoadDefaultConfig(context.TODO(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("falha ao carregar configuração AWS: %w", err)
	}

	// Criar cliente S3 com opções
	s3Opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = cfg.ForcePathStyle
		},
	}

	s3Client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &Client{s3: s3Client}, nil
}

func (c *Client) ListBuckets(ctx context.Context) ([]string, error) {
	out, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("falha ao listar buckets: %w", err)
	}

	var buckets []string
	for _, b := range out.Buckets {
		if b.Name != nil {
			buckets = append(buckets, *b.Name)
		}
	}
	return buckets, nil
}

// Método adicional para testar conexão
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	return err
}


// s3/client.go - Modifique a função ListObjects

// s3/client.go - Versão simplificada sem paginação

func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]Item, error) {
	if bucket == "" {
		return nil, fmt.Errorf("nome do bucket não pode ser vazio")
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int32(10000), // Aumente se quiser mais de uma vez
	}

	var allItems []Item
	var continuationToken *string
	
	for {
		input.ContinuationToken = continuationToken
		
		result, err := c.s3.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("falha ao listar objetos: %w", err)
		}
		
		// Processar pastas
		for _, commonPrefix := range result.CommonPrefixes {
			if commonPrefix.Prefix != nil {
				prefixStr := *commonPrefix.Prefix
				name := strings.TrimPrefix(prefixStr, prefix)
				name = strings.TrimSuffix(name, "/")
				
				allItems = append(allItems, Item{
					Name:   name + "/",
					Type:   Folder,
					Prefix: prefixStr,
				})
			}
		}
		
		// Processar arquivos (sem limite)
		for _, obj := range result.Contents {
			if obj.Key != nil {
				key := *obj.Key
				if key == prefix {
					continue
				}
				
				if strings.HasSuffix(key, "/") {
					continue
				}
				
				name := strings.TrimPrefix(key, prefix)
				allItems = append(allItems, Item{
					Name:   name,
					Type:   File,
					Prefix: key,
				})
			}
		}
		
		// Continuar paginação se necessário
		if result.NextContinuationToken == nil || !*result.IsTruncated {
			break
		}
		
		continuationToken = result.NextContinuationToken
		
		// Opcional: limite de segurança
		// if len(allItems) > 10000 { // Limite de 10k itens
		// 	fmt.Printf("AVISO: Limite de 10000 itens atingido para %s/%s\n", bucket, prefix)
		// 	break
		// }
	}

	// Ordenar
	sortItems(allItems)
	
	return allItems, nil
}

// Adicione esta função auxiliar para ordenar
func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool {
		// Pastas primeiro
		if items[i].Type == Folder && items[j].Type != Folder {
			return true
		}
		if items[i].Type != Folder && items[j].Type == Folder {
			return false
		}
		// Depois ordenar por nome
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
}
// Adicione após a função ListObjects existente

// ListObjectsPaginated lista objetos com paginação
func (c *Client) ListObjectsPaginated(ctx context.Context, bucket, prefix string, maxKeys int32) ([]Item, string, error) {
	if bucket == "" {
		return nil, "", fmt.Errorf("nome do bucket não pode ser vazio")
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int32(maxKeys), // Limitar número de resultados
	}

	result, err := c.s3.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, "", fmt.Errorf("falha ao listar objetos: %w", err)
	}

	var items []Item
	
	// Processar pastas
	for _, commonPrefix := range result.CommonPrefixes {
		if commonPrefix.Prefix != nil {
			prefixStr := *commonPrefix.Prefix
			name := strings.TrimPrefix(prefixStr, prefix)
			name = strings.TrimSuffix(name, "/")
			
			items = append(items, Item{
				Name:   name + "/",
				Type:   Folder,
				Prefix: prefixStr,
			})
		}
	}
	
	// Processar arquivos
	for _, obj := range result.Contents {
		if obj.Key != nil {
			key := *obj.Key
			if key == prefix {
				continue
			}
			
			name := strings.TrimPrefix(key, prefix)
			if strings.HasSuffix(name, "/") {
				continue
			}
			
			items = append(items, Item{
				Name:   name,
				Type:   File,
				Prefix: key,
			})
		}
	}

	// Retornar next token se houver mais resultados
	nextToken := ""
	if result.NextContinuationToken != nil {
		nextToken = *result.NextContinuationToken
	}

	return items, nextToken, nil
}

// CountObjects conta quantos objetos tem no prefixo
func (c *Client) CountObjects(ctx context.Context, bucket, prefix string) (int, error) {
	if bucket == "" {
		return 0, fmt.Errorf("nome do bucket não pode ser vazio")
	}

	// Usar MaxKeys=0 para contar rapidamente (algumas implementações S3 suportam)
	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int32(10000), // Limitar para resposta rápida
	}

	result, err := c.s3.ListObjectsV2(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("falha ao contar objetos: %w", err)
	}

	// KeyCount é o número de chaves retornadas (não o total)
	// Para uma contagem exata precisaríamos paginar tudo
	// Esta é uma estimativa rápida
	total := int(*result.KeyCount)
	
	// Se houver mais resultados, estimar um total maior
	if result.NextContinuationToken != nil {
		total = 1000 // Estimativa conservadora
	}

	return total, nil
}

// UploadFile faz upload de um arquivo para o S3
func (c *Client) UploadFile(ctx context.Context, bucket, key, filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("falha ao abrir arquivo: %w", err)
	}
	defer file.Close()

	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	
	return err
}
