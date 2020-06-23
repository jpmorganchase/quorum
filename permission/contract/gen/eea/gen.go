// Quorum
//
// this is to generate go binding for smart contracts used in permissioning
//
// Require:
// 1. solc 0.5.4
// 2. abigen (make all from root)

//go:generate solc --abi --bin -o . --overwrite ../AccountManager.sol
//go:generate solc --abi --bin -o . --overwrite ../NodeManager.sol
//go:generate solc --abi --bin -o . --overwrite ../OrgManager.sol
//go:generate solc --abi --bin -o . --overwrite ../PermissionsImplementation.sol
//go:generate solc --abi --bin -o . --overwrite ../PermissionsInterface.sol
//go:generate solc --abi --bin -o . --overwrite ../PermissionsUpgradable.sol
//go:generate solc --abi --bin -o . --overwrite ../RoleManager.sol
//go:generate solc --abi --bin -o . --overwrite ../VoterManager.sol

//go:generate abigen -pkg eea -abi  ./AccountManager.abi            -bin  ./AccountManager.bin            -type EeaAcctManager   -out ../../../bind/eea/accounts.go
//go:generate abigen -pkg eea -abi  ./NodeManager.abi               -bin  ./NodeManager.bin               -type EeaNodeManager   -out ../../../bind/eea/nodes.go
//go:generate abigen -pkg eea -abi  ./OrgManager.abi                -bin  ./OrgManager.bin                -type EeaOrgManager    -out ../../../bind/eea/org.go
//go:generate abigen -pkg eea -abi  ./PermissionsImplementation.abi -bin  ./PermissionsImplementation.bin -type EeaPermImpl      -out ../../../bind/eea/permission_impl.go
//go:generate abigen -pkg eea -abi  ./PermissionsInterface.abi      -bin  ./PermissionsInterface.bin      -type EeaPermInterface -out ../../../bind/eea/permission_interface.go
//go:generate abigen -pkg eea -abi  ./PermissionsUpgradable.abi     -bin  ./PermissionsUpgradable.bin     -type EeaPermUpgr      -out ../../../bind/eea/permission_upgr.go
//go:generate abigen -pkg eea -abi  ./RoleManager.abi               -bin  ./RoleManager.bin               -type EeaRoleManager   -out ../../../bind/eea/roles.go
//go:generate abigen -pkg eea -abi  ./VoterManager.abi              -bin  ./VoterManager.bin              -type EeaVoterManager  -out ../../../bind/eea/voter.go

package eea
