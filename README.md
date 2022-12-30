# json_contract_interface
(A piece of)  a contract interface for geth/bor meant to receive JSON ABIs and JSON call arguments from external sources.  The script uses the ABI to create a data structure that the "call arguments" JSON string can be unmarshalled into in a format that is compatible with the ABI package's encoding (which it then uses to pack the arguments).

This is NOT meant to be its own module, but rather should be implemented into other modules to allow compatibility with JSONs sent from external sources that need to be unmarshalled.

It's kept out its own package because it should, ideally, be placed where it can both 1. receive external data, and 2. send the new TransactionArgs to a signing module.
