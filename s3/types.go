// s3/types.go (crie este arquivo)
package s3

type ItemType int

const (
	Bucket ItemType = iota
	Folder
	File
)

type Item struct {
	Name   string
	Type   ItemType
	Prefix string
}