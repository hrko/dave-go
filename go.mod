module github.com/thomas-vilte/dave-go

go 1.26.0

require (
	github.com/disgoorg/godave v0.2.0
	github.com/thomas-vilte/mls-go v1.6.0
)

require (
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/thomas-vilte/mls-go => github.com/hrko/mls-go v1.6.1-0.20261111020018-634fb81d9a18
