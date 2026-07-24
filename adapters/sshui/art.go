package sshui

// Trusty, hand-drawn as a character map (from Guillermo's trusty_smile.txt):
// each character is one cell rendered as a background-colored space, and the
// palette below mirrors render_trusty.sh. Colors are ANSI-256 indices; they
// render through the session renderer, so colorless terminals degrade
// cleanly instead of seeing escape codes.
var trustyMap = []string{
	`      ████████████████████████      `,
	`    ██▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒██    `,
	`    ██▒▒▒▒▒▒████████████▒▒▒▒▒▒██    `,
	`    ██████████▒▒▒▒▒▒▒▒██████████    `,
	`  ██wwOOOOOOOO████████OOOOOOOOww██  `,
	`██wwOOOO██OOOO██▒▒▒▒██OOOO██OOOOww██`,
	`██OOOOOOOOOO██▒▒▒▒▒▒▒▒██OOOOOOOOOO██`,
	`  ██OOOOOO██▒▒▒▒██▒▒▒▒▒▒██OOOOOO██  `,
	`    ██████▒▒▒▒▒▒▒▒████▒▒▒▒████████  `,
	`    ██▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒██    `,
	`    ██▒▒pp▒▒pp▒▒▒▒▒▒▒▒▒▒SS▒▒▒▒██    `,
	`    ██▒▒pp▒▒pp▒▒▒▒▒▒▒▒SSSSSS▒▒██    `,
	`    ██▒▒▒▒▒▒pp▒▒▒▒▒▒▒▒▒▒SS▒▒▒▒██    `,
	`    ██▒▒rr▒▒pp▒▒▒▒▒▒▒▒SS▒▒SS▒▒██    `,
	`    ██▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒██    `,
	`      ████████████████████████      `,
}

// trustyPalette maps each map character to its ANSI-256 background color;
// characters missing here (and spaces) render transparent.
var trustyPalette = map[rune]string{
	'█': "16",  // outline
	'▒': "222", // fur
	'O': "208", // ears and brow
	'R': "203", // red accents
	'Y': "172", // dark fur
	'w': "231", // ear tips
	'm': "236", // mouth
	'p': "208",
	'r': "203",
	'S': "208",
}
