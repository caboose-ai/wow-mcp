
TestAddonDB = {
	["profiles"] = {
		["Default"] = {
			["scale"] = 1.25,
			["enabled"] = true,
			["name"] = "Quote \" and backslash \\ and pipe |cff00ff00x|r and tab\tend",
			["list"] = {
				"a", -- [1]
				"b", -- [2]
				"c", -- [3]
			},
			["count"] = -3,
			["tiny"] = 1e-05,
			["big"] = 1234567890123,
			["inf"] = 1/0,
			["neginf"] = -1/0,
			["nested"] = {
				[1] = "one",
				[2] = "two",
				["x"] = nil,
				[true] = "boolkey",
			},
			[10] = "sparse",
		},
	},
	["profileKeys"] = {
		["Testchar - Testrealm"] = "Default",
	},
	["version"] = 3,
}
TestAddonGlobal = 42
