package phone

import "strings"

// The North American Numbering Plan's area codes: whose they are (the
// country, and the state or province of the US's and Canada's), as the
// NANPA assigned them by 2026.

type place struct{ country, region string }

// areas are the area codes in service, by country and region.
var areas = []struct {
	country, region, codes string
}{
	{"US", "Alabama", "205 251 256 334 483 659 938"},
	{"US", "Alaska", "907"},
	{"US", "Arizona", "480 520 602 623 928"},
	{"US", "Arkansas", "327 479 501 870"},
	{"US", "California", "209 213 279 310 323 341 350 357 369 408 415 424 442 510 530 559 562 619 626 628 650 657 661 " +
		"669 707 714 738 747 760 805 818 820 831 837 840 858 909 916 925 949 951"},
	{"US", "Colorado", "303 719 720 748 970 983"},
	{"US", "Connecticut", "203 475 860 959"},
	{"US", "Delaware", "302"},
	{"US", "District of Columbia", "202 771"},
	{"US", "Florida", "239 305 321 324 352 386 407 448 561 645 656 689 727 728 754 772 786 813 850 863 904 941 954"},
	{"US", "Georgia", "229 404 470 478 678 706 762 770 912 943"},
	{"US", "Hawaii", "808"},
	{"US", "Idaho", "208 986"},
	{"US", "Illinois", "217 224 309 312 331 447 464 618 630 708 730 773 779 815 847 861 872"},
	{"US", "Indiana", "219 260 317 463 574 765 812 930"},
	{"US", "Iowa", "319 515 563 641 712"},
	{"US", "Kansas", "316 620 785 913"},
	{"US", "Kentucky", "270 364 502 606 859"},
	{"US", "Louisiana", "225 318 337 457 504 985"},
	{"US", "Maine", "207"},
	{"US", "Maryland", "227 240 301 410 443 667"},
	{"US", "Massachusetts", "339 351 413 508 617 774 781 857 978"},
	{"US", "Michigan", "231 248 269 313 517 586 616 679 734 810 906 947 989"},
	{"US", "Minnesota", "218 320 507 612 651 763 924 952"},
	{"US", "Mississippi", "228 601 662 769"},
	{"US", "Missouri", "235 314 417 557 573 636 660 816 975"},
	{"US", "Montana", "406"},
	{"US", "Nebraska", "308 402 531"},
	{"US", "Nevada", "702 725 775"},
	{"US", "New Hampshire", "603"},
	{"US", "New Jersey", "201 551 609 640 732 848 856 862 908 973"},
	{"US", "New Mexico", "505 575"},
	{"US", "New York", "212 315 329 332 347 363 516 518 585 607 624 631 646 680 716 718 838 845 914 917 929 934"},
	{"US", "North Carolina", "252 336 472 704 743 828 910 919 980 984"},
	{"US", "North Dakota", "701"},
	{"US", "Ohio", "216 220 234 283 326 330 380 419 436 440 513 567 614 740 937"},
	{"US", "Oklahoma", "405 539 572 580 918"},
	{"US", "Oregon", "458 503 541 971"},
	{"US", "Pennsylvania", "215 223 267 272 412 445 484 570 582 610 717 724 814 835 878"},
	{"US", "Rhode Island", "401"},
	{"US", "South Carolina", "803 821 839 843 854 864"},
	{"US", "South Dakota", "605"},
	{"US", "Tennessee", "423 615 629 731 865 901 931"},
	{"US", "Texas", "210 214 254 281 325 346 361 409 430 432 469 512 682 713 726 737 806 817 830 832 903 915 " +
		"936 940 945 956 972 979"},
	{"US", "Utah", "385 435 801"},
	{"US", "Vermont", "802"},
	{"US", "Virginia", "276 434 540 571 686 703 757 804 826 948"},
	{"US", "Washington", "206 253 360 425 509 564"},
	{"US", "West Virginia", "304 681"},
	{"US", "Wisconsin", "262 274 353 414 534 608 715 920"},
	{"US", "Wyoming", "307"},
	{"PR", "", "787 939"},
	{"VI", "", "340"},
	{"GU", "", "671"},
	{"MP", "", "670"},
	{"AS", "", "684"},

	{"CA", "Alberta", "368 403 587 780 825"},
	{"CA", "British Columbia", "236 250 257 604 672 778"},
	{"CA", "Manitoba", "204 431 584"},
	{"CA", "New Brunswick", "428 506"},
	{"CA", "Newfoundland and Labrador", "709 879"},
	{"CA", "Nova Scotia and Prince Edward Island", "782 902"},
	{"CA", "Ontario", "226 249 289 343 365 382 416 437 519 548 613 647 683 705 742 753 807 905 942"},
	{"CA", "Quebec", "263 354 367 418 438 450 468 514 579 581 819 873"},
	{"CA", "Saskatchewan", "306 474 639"},
	{"CA", "Yukon, Northwest Territories and Nunavut", "867"},
	{"CA", "", "600 622"},

	{"AG", "", "268"}, {"AI", "", "264"}, {"BS", "", "242"}, {"BB", "", "246"}, {"BM", "", "441"},
	{"VG", "", "284"}, {"KY", "", "345"}, {"DM", "", "767"}, {"DO", "", "809 829 849"},
	{"GD", "", "473"}, {"JM", "", "658 876"}, {"MS", "", "664"}, {"KN", "", "869"}, {"LC", "", "758"},
	{"VC", "", "784"}, {"SX", "", "721"}, {"TT", "", "868"}, {"TC", "", "649"},
}

// nanpServices are the area codes that are services rather than places.
var nanpServices = map[string]string{
	"800": "toll-free", "833": "toll-free", "844": "toll-free", "855": "toll-free",
	"866": "toll-free", "877": "toll-free", "888": "toll-free",
	"900": "premium", "700": "carrier services", "710": "US government",
	"500": "personal", "521": "personal", "522": "personal", "523": "personal", "524": "personal",
	"525": "personal", "526": "personal", "527": "personal", "528": "personal", "529": "personal",
	"532": "personal", "533": "personal", "544": "personal", "566": "personal", "577": "personal",
	"588": "personal",
}

var areaPlace = func() map[string]place {
	m := map[string]place{}
	for _, a := range areas {
		for _, c := range strings.Fields(a.codes) {
			m[c] = place{a.country, a.region}
		}
	}
	return m
}()
