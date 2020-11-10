package security

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/jpmorganchase/quorum-security-plugin-sdk-go/proto"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.Root().SetHandler(log.StreamHandler(os.Stdout, log.TerminalFormat(false)))
}

type testCase struct {
	msg            string
	rawAuthorities []string
	attributes     []*ContractSecurityAttribute
	isAuthorized   bool
}

func TestMatch_whenTypical(t *testing.T) {
	granted, _ := url.Parse("private://0xa1b1c1/create/contracts?from.tm=A/")
	ask, _ := url.Parse("private://0xa1b1c1/create/contracts?from.tm=A%2F")

	assert.True(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))
}

func TestMatch_whenAnyAction(t *testing.T) {
	granted, _ := url.Parse("private://0xa1b1c1/_/contracts?owned.eoa=0x0&from.tm=A1")
	ask, _ := url.Parse("private://0xa1b1c1/read/contracts?from.tm=A1")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))

	ask, _ = url.Parse("private://0xa1b1c1/read/contracts?owned.eoa=0x0&from.tm=A1&from.tm=B1")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))

	ask, _ = url.Parse("private://0xa1b1c1/write/contracts?owned.eoa=0x0&from.tm=A1")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "write",
	}, ask, granted))
}

func TestMatch_whenPathNotMatched(t *testing.T) {
	granted, _ := url.Parse("private://0xa1b1c1/write/contracts?owned.eoa=0x0&from.tm=A1")
	ask, _ := url.Parse("private://0xa1b1c1/read/contracts?from.tm=A1")

	assert.False(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))
}

func TestMatch_whenSchemeIsNotEqual(t *testing.T) {
	granted, _ := url.Parse("unknown://0xa1b1c1/create/contracts?from.tm=A")
	ask, _ := url.Parse("private://0xa1b1c1/create/contracts?from.tm=A")

	assert.False(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))
}

func TestMatch_whenContractWritePermission_GrantedIsTheSuperSet(t *testing.T) {
	granted, _ := url.Parse("private://0x0/write/contracts?owned.eoa=0x0&from.tm=A&from.tm=B")
	ask, _ := url.Parse("private://0x0/write/contracts?owned.eoa=0x0&from.tm=A")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "write",
	}, ask, granted), "with write permission")

	granted, _ = url.Parse("private://0x0/read/contracts?owned.eoa=0x0&from.tm=A&from.tm=B")
	ask, _ = url.Parse("private://0x0/read/contracts?owned.eoa=0x0&from.tm=A")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted), "with read permission")
}

func TestMatch_whenContractReadPermission_EoaSame(t *testing.T) {
	granted, _ := url.Parse("private://0x0/read/contracts?owned.eoa=0x095e7baea6a6c7c4c2dfeb977efac326af552d87")
	ask, _ := url.Parse("private://0x0/read/contracts?owned.eoa=0x945304eb96065b2a98b57a48a06ae28d285a71b5")

	assert.False(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))
}

func TestMatch_whenContractReadPermission_EoaDifferent(t *testing.T) {
	granted, _ := url.Parse("private://0x0/read/contracts?owned.eoa=0x095e7baea6a6c7c4c2dfeb977efac326af552d87")
	ask, _ := url.Parse("private://0x0/read/contracts?owned.eoa=0x095e7baea6a6c7c4c2dfeb977efac326af552d87")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))
}

func TestMatch_whenContractReadPermission_TmKeysIntersect(t *testing.T) {
	granted, _ := url.Parse("private://0x0/read/contracts?from.tm=A&from.tm=B")
	ask, _ := url.Parse("private://0x0/read/contracts?from.tm=B&from.tm=C")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))
}

func TestMatch_whenContractReadPermission_TmKeysDontIntersect(t *testing.T) {
	granted, _ := url.Parse("private://0x0/read/contracts?from.tm=A&from.tm=B")
	ask, _ := url.Parse("private://0x0/read/contracts?from.tm=C&from.tm=D")

	assert.False(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "read",
	}, ask, granted))
}

func TestMatch_whenContractWritePermission_Same(t *testing.T) {
	granted, _ := url.Parse("private://0x0/write/contracts?owned.eoa=0x0&from.tm=A")
	ask, _ := url.Parse("private://0x0/write/contracts?owned.eoa=0x0&from.tm=A")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "write",
	}, ask, granted))
}

func TestMatch_whenContractWritePermission_Different(t *testing.T) {
	granted, _ := url.Parse("private://0x0/write/contracts?owned.eoa=0x0&from.tm=A")
	ask, _ := url.Parse("private://0x0/write/contracts?owned.eoa=0x0&from.tm=B")

	assert.False(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "write",
	}, ask, granted))
}

func TestMatch_whenContractCreatePermission_Same(t *testing.T) {
	granted, _ := url.Parse("private://0x0/create/contracts?owned.eoa=0x0&from.tm=A")
	ask, _ := url.Parse("private://0x0/create/contracts?owned.eoa=0x0&from.tm=A")

	assert.True(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "create",
	}, ask, granted))
}

func TestMatch_whenContractCreatePermission_Different(t *testing.T) {
	granted, _ := url.Parse("private://0x0/create/contracts?owned.eoa=0x0&from.tm=A")
	ask, _ := url.Parse("private://0x0/create/contracts?owned.eoa=0x0&from.tm=B")

	assert.False(t, match(&ContractSecurityAttribute{
		Visibility: "private",
		Action:     "create",
	}, ask, granted))
}

func TestMatch_whenUsingWildcardAccount(t *testing.T) {
	granted, _ := url.Parse("private://0x0/create/contracts?from.tm=dLHrFQpbSda0EhJnLonsBwDjks%2Bf724NipfI5zK5RSs%3D")
	ask, _ := url.Parse("private://0xed9d02e382b34818e88b88a309c7fe71e65f419d/create/contracts?from.tm=dLHrFQpbSda0EhJnLonsBwDjks%2Bf724NipfI5zK5RSs%3D")

	assert.True(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))

	granted, _ = url.Parse("private://0x0/read/contract?owned.eoa=0x0")
	ask, _ = url.Parse("private://0xa1b1c1/read/contract?owned.eoa=0x1234")

	assert.True(t, match(&ContractSecurityAttribute{Action: "read"}, ask, granted))
}

func TestMatch_whenNotUsingWildcardAccount(t *testing.T) {
	granted, _ := url.Parse("private://0xed9d02e382b34818e88b88a309c7fe71e65f419d/create/contracts?from.tm=dLHrFQpbSda0EhJnLonsBwDjks%2Bf724NipfI5zK5RSs%3D")
	ask, _ := url.Parse("private://0xed9d02e382b34818e88b88a309c7fe71e65f419d/create/contracts?from.tm=dLHrFQpbSda0EhJnLonsBwDjks%2Bf724NipfI5zK5RSs%3D")

	assert.True(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))

	granted, _ = url.Parse("private://0x0/read/contract?owned.eoa=0x0")
	ask, _ = url.Parse("private://0xa1b1c1/read/contract?owned.eoa=0x1234")

	assert.True(t, match(&ContractSecurityAttribute{Action: "read"}, ask, granted))
}

func TestMatch_failsWhenAccountsDiffer(t *testing.T) {
	granted, _ := url.Parse("private://0xed9d02e382b34818e88b88a309c7fe71e65f419d/create/contracts?from.tm=dLHrFQpbSda0EhJnLonsBwDjks%2Bf724NipfI5zK5RSs%3D")
	ask, _ := url.Parse("private://0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b/create/contracts?from.tm=dLHrFQpbSda0EhJnLonsBwDjks%2Bf724NipfI5zK5RSs%3D")

	assert.False(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))
}

func TestMatch_whenPublic(t *testing.T) {
	granted, _ := url.Parse("private://0xa1b1c1/create/contract?from.tm=A/")
	ask, _ := url.Parse("public://0x0/create/contract")

	assert.True(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))
}

func TestMatch_whenNotEscaped(t *testing.T) {
	// query not escaped probably in the granted authority resource identitifer
	granted, _ := url.Parse("private://0xed9d02e382b34818e88b88a309c7fe71e65f419d/create/contracts?from.tm=BULeR8JyUWhiuuCMU/HLA0Q5pzkYT+cHII3ZKBey3Bo=")
	ask, _ := url.Parse("private://0xed9d02e382b34818e88b88a309c7fe71e65f419d/create/contracts?from.tm=BULeR8JyUWhiuuCMU%2FHLA0Q5pzkYT%2BcHII3ZKBey3Bo%3D")

	assert.False(t, match(&ContractSecurityAttribute{Action: "create"}, ask, granted))
}

func runTestCases(t *testing.T, testCases []*testCase) {
	testObject := &DefaultContractAccessDecisionManager{}
	for _, tc := range testCases {
		log.Debug("--> Running test case: " + tc.msg)
		authorities := make([]*proto.GrantedAuthority, 0)
		for _, a := range tc.rawAuthorities {
			authorities = append(authorities, &proto.GrantedAuthority{Raw: a})
		}
		b, err := testObject.IsAuthorized(
			context.Background(),
			&proto.PreAuthenticatedAuthenticationToken{Authorities: authorities},
			tc.attributes)
		assert.NoError(t, err, tc.msg)
		assert.Equal(t, tc.isAuthorized, b, tc.msg)
	}
}

func TestDefaultAccountAccessDecisionManager_IsAuthorized_forPublicContracts(t *testing.T) {
	runTestCases(t, []*testCase{
		canCreatePublicContracts,
		// canNotCreatePublicContracts,
		canReadOwnedPublicContracts,
		canReadOtherPublicContracts,
		// canNotReadOtherPublicContracts,
		canWriteOwnedPublicContracts,
		canWriteOtherPublicContracts1,
		canWriteOtherPublicContracts2,
		// canNotWriteOtherPublicContracts,
		canCreatePublicContractsAndWriteToOthers,
	})
}

func TestDefaultAccountAccessDecisionManager_IsAuthorized_forPrivateContracts(t *testing.T) {
	runTestCases(t, []*testCase{
		canCreatePrivateContracts,
		canNotCreatePrivateContracts,
		canReadOwnedPrivateContracts,
		canReadOtherPrivateContracts,
		canNotReadOtherPrivateContracts,
		canNotReadOtherPrivateContractsNoPrivy,
		canWriteOwnedPrivateContracts,
		canWriteOtherPrivateContracts,
		canWriteOtherPrivateContractsWithOverlappedScope,
		canNotWriteOtherPrivateContracts,
		canNotWriteOtherPrivateContractsNoPrivy,
	})
}

var (
	canCreatePublicContracts = &testCase{
		msg: "0x0a1a1a1 can create public contracts",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/create/contracts",
		},
		attributes: []*ContractSecurityAttribute{
			{
				AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
					From: common.HexToAddress("0xa1a1a1"),
				},
				Visibility: "public",
				Action:     "create",
			},
		},
		isAuthorized: true,
	}
	canCreatePublicContractsAndWriteToOthers = &testCase{
		msg: "0x0a1a1a1 can create public contracts and write to contracts created by 0xb1b1b1",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/create/contracts",
			"public://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&owned.eoa=0x0000000000000000000000000000000000c1c1c1",
		},
		attributes: []*ContractSecurityAttribute{
			{
				AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
					From: common.HexToAddress("0xa1a1a1"),
				},
				Visibility: "public",
				Action:     "create",
			}, {
				AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
					From: common.HexToAddress("0xa1a1a1"),
					To:   common.HexToAddress("0xb1b1b1"),
				},
				Visibility: "public",
				Action:     "write",
			},
		},
		isAuthorized: true,
	}
	//
	//canNotCreatePublicContracts = &testCase{
	//	msg: "0xb1b1b1 can not create public contracts",
	//	rawAuthorities: []string{
	//		"public://0x0000000000000000000000000000000000a1a1a1/create/contracts",
	//		"public://0x0000000000000000000000000000000000b1b1b1/read/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1",
	//	},
	//	attributes: []*ContractSecurityAttribute{{
	//		AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
	//			From: common.HexToAddress("0xb1b1b1"),
	//		},
	//		Visibility: "public",
	//		Action:     "create",
	//	}},
	//	isAuthorized: false,
	//}
	canReadOwnedPublicContracts = &testCase{
		msg: "0x0a1a1a1 can read public contracts created by self",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
			},
			Visibility: "public",
			Action:     "read",
		}},
		isAuthorized: true,
	}
	canReadOtherPublicContracts = &testCase{
		msg: "0x0a1a1a1 can read public contracts created by 0xb1b1b1",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "public",
			Action:     "read",
		}},
		isAuthorized: true,
	}
	//canNotReadOtherPublicContracts = &testCase{
	//	msg: "0x0a1a1a1 can only read public contracts created by self",
	//	rawAuthorities: []string{
	//		"public://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1",
	//	},
	//	attributes: []*ContractSecurityAttribute{{
	//		AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
	//			From: common.HexToAddress("0xa1a1a1"),
	//			To:   common.HexToAddress("0xb1b1b1"),
	//		},
	//		Visibility: "public",
	//		Action:     "read",
	//	}},
	//	isAuthorized: false,
	//}
	canWriteOwnedPublicContracts = &testCase{
		msg: "0x0a1a1a1 can send transactions to public contracts created by self",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
			},
			Visibility: "public",
			Action:     "write",
		}},
		isAuthorized: true,
	}
	canWriteOtherPublicContracts1 = &testCase{
		msg: "0xa1a1a1 can send transactions to public contracts created by 0xb1b1b1",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&owned.eoa=0x0000000000000000000000000000000000c1c1c1",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "public",
			Action:     "write",
		}},
		isAuthorized: true,
	}
	canWriteOtherPublicContracts2 = &testCase{
		msg: "0xa1a1a1 can send transactions to public contracts created by 0xb1b1b1",
		rawAuthorities: []string{
			"public://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&owned.eoa=0x0000000000000000000000000000000000c1c1c1",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xc1c1c1"),
			},
			Visibility: "public",
			Action:     "write",
		}},
		isAuthorized: true,
	}
	//canNotWriteOtherPublicContracts = &testCase{
	//	msg: "0x0a1a1a1 can only send transactions to public contracts created by self",
	//	rawAuthorities: []string{
	//		"public://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1",
	//		"public://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1",
	//	},
	//	attributes: []*ContractSecurityAttribute{{
	//		AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
	//			From: common.HexToAddress("0xa1a1a1"),
	//			To:   common.HexToAddress("0xb1b1b1"),
	//		},
	//		Visibility: "public",
	//		Action:     "write",
	//	}},
	//	isAuthorized: false,
	//}
	// private contracts
	canCreatePrivateContracts = &testCase{
		msg: "0x0a1a1a1 can create private contracts with sender key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/create/contracts?from.tm=A",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
			},
			Visibility:  "private",
			Action:      "create",
			PrivateFrom: "A",
			Parties:     []string{},
		}},
		isAuthorized: true,
	}
	canNotCreatePrivateContracts = &testCase{
		msg: "0x0a1a1a1 can NOT create private contracts with sender key A if only own key B",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/create/contracts?from.tm=B",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
			},
			Visibility:  "private",
			Action:      "create",
			PrivateFrom: "A",
			Parties:     []string{},
		}},
		isAuthorized: false,
	}
	canReadOwnedPrivateContracts = &testCase{
		msg: "0x0a1a1a1 can read private contracts created by self and was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1&from.tm=A&from.tm=B",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
			},
			Visibility: "private",
			Action:     "read",
			Parties:    []string{"A"},
		}},
		isAuthorized: true,
	}
	canReadOtherPrivateContracts = &testCase{
		msg: "0x0a1a1a1 can read private contracts created by 0xb1b1b1 and was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&from.tm=A",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "private",
			Action:     "read",
			Parties:    []string{"A"},
		}},
		isAuthorized: true,
	}
	canNotReadOtherPrivateContracts = &testCase{
		msg: "0x0a1a1a1 can NOT read private contracts created by 0xb1b1b1 even it was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000c1c1c1&from.tm=A",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "private",
			Action:     "read",
			Parties:    []string{"A"},
		}},
		isAuthorized: false,
	}
	canNotReadOtherPrivateContractsNoPrivy = &testCase{
		msg: "0x0a1a1a1 can NOT read private contracts created by 0xb1b1b1 as it was privy to a key B",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/read/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&from.tm=B",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "private",
			Action:     "read",
			Parties:    []string{"A"},
		}},
		isAuthorized: false,
	}
	canWriteOwnedPrivateContracts = &testCase{
		msg: "0x0a1a1a1 can write private contracts created by self and was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000a1a1a1&from.tm=A&from.tm=B",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
			},
			Visibility:  "private",
			Action:      "write",
			PrivateFrom: "A",
			Parties:     []string{"A"},
		}},
		isAuthorized: true,
	}
	canWriteOtherPrivateContracts = &testCase{
		msg: "0x0a1a1a1 can write private contracts created by 0xb1b1b1 and was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&from.tm=A",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility:  "private",
			Action:      "write",
			PrivateFrom: "A",
			Parties:     []string{"A"},
		}},
		isAuthorized: true,
	}
	canWriteOtherPrivateContractsWithOverlappedScope = &testCase{
		msg: "0x0a1a1a1 can write private contracts created by 0xb1b1b1 and was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&from.tm=A",
			"private://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&from.tm=A&from.tm=B",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility:  "private",
			Action:      "write",
			PrivateFrom: "A",
			Parties:     []string{"A"},
		}},
		isAuthorized: true,
	}
	canNotWriteOtherPrivateContracts = &testCase{
		msg: "0x0a1a1a1 can NOT write private contracts created by 0xb1b1b1 even it was privy to a key A",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000c1c1c1&from.tm=A",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "private",
			Action:     "write",
			Parties:    []string{"A"},
		}},
		isAuthorized: false,
	}
	canNotWriteOtherPrivateContractsNoPrivy = &testCase{
		msg: "0x0a1a1a1 can NOT write private contracts created by 0xb1b1b1 as it was privy to a key B",
		rawAuthorities: []string{
			"private://0x0000000000000000000000000000000000a1a1a1/write/contracts?owned.eoa=0x0000000000000000000000000000000000b1b1b1&from.tm=B",
		},
		attributes: []*ContractSecurityAttribute{{
			AccountStateSecurityAttribute: &AccountStateSecurityAttribute{
				From: common.HexToAddress("0xa1a1a1"),
				To:   common.HexToAddress("0xb1b1b1"),
			},
			Visibility: "private",
			Action:     "write",
			Parties:    []string{"A"},
		}},
		isAuthorized: false,
	}
)