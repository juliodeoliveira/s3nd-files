// s3/client.go
package s3

import (
	"context"
	"fmt"
	"os"
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
	UseSSL    bool // Adicione este campo
}

func New(cfg Config) (*Client, error) {
	// Crie um resolvedor de endpoint customizado para o MinIO
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if service == s3.ServiceID {
			return aws.Endpoint{
				URL:               cfg.Endpoint,
				SigningRegion:     cfg.Region,
				HostnameImmutable: true, // Importante para MinIO
			}, nil
		}
		// Retornando EndpointNotFoundError fará o SDK usar o padrão
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	// Configure as credenciais
	creds := credentials.NewStaticCredentialsProvider(
		cfg.AccessKey,
		cfg.SecretKey,
		"",
	)

	// Carregue a configuração
	awsCfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("falha ao carregar configuração AWS: %w", err)
	}

	// Crie o cliente S3
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// Use path style se necessário (comum para MinIO)
		o.UsePathStyle = true
	})

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


func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]Item, error) {
	if bucket == "" {
		return nil, fmt.Errorf("nome do bucket não pode ser vazio")
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"), // Usar delimiter para separar pastas
	}

	result, err := c.s3.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar objetos: %w", err)
	}

	var items []Item
	
	// Primeiro, processar os prefixes (pastas)
	for _, commonPrefix := range result.CommonPrefixes {
		if commonPrefix.Prefix != nil {
			prefixStr := *commonPrefix.Prefix
			// Extrair o nome da pasta do prefixo completo
			name := strings.TrimPrefix(prefixStr, prefix)
			name = strings.TrimSuffix(name, "/")
			
			items = append(items, Item{
				Name:   name + "/",
				Type:   Folder,
				Prefix: prefixStr,
			})
		}
	}
	
	// Depois, processar os objetos (arquivos)
	for _, obj := range result.Contents {
		if obj.Key != nil {
			key := *obj.Key
			// Pular se for o próprio prefixo (pasta "vazia")
			if key == prefix {
				continue
			}
			
			name := strings.TrimPrefix(key, prefix)
			// Se terminar com "/", é uma pasta já coberta por CommonPrefixes
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

	return items, nil
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
		MaxKeys:   aws.Int32(1000), // Limitar para resposta rápida
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



