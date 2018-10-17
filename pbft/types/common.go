package types

const (
	Transaction_UNDEFINED Transaction_Type = 0
	// deploy a chaincode to the network and call `Init` function
	Transaction_CHAINCODE_DEPLOY Transaction_Type = 1
	// call a chaincode `Invoke` function as a transaction
	Transaction_CHAINCODE_INVOKE Transaction_Type = 2
	// call a chaincode `query` function
	Transaction_CHAINCODE_QUERY Transaction_Type = 3
	// terminate a chaincode; not implemented yet
	Transaction_CHAINCODE_TERMINATE Transaction_Type = 4
)
