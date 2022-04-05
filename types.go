package main
type JAccount struct {
	AccId		string	`json:"-"`
	Login 		string	`json:"login,omitempty"`	
	Passwd 		string	`json:"password,omitempty"`
	Session 	string	`json:"session,omitempty"`
	AccProxy 	string	`json:"proxy,omitempty"`
}

type JSearcherTask struct{
	JAccount
	Id				uint		`json:"id"`
	CountryCode		string		`json:"countryCode"`
	Proxy 			string		`json:"proxy"`
}

type JTaskRegion struct {
	CountryCode string	`json:"countryCode,omitempty"`
	City		string	`json:"city,omitempty"`
}
type JTask struct{
	JAccount
	JTaskRegion
	RuleIDs 		[]string	`json:"ruleIds,omitempty"`
	Id				string		`json:"id,omitempty"`
	Proxy 			string		`json:"proxy,omitempty"`
	SearchWords 	string		`json:"searchWords,omitempty"`
	Uri				string		`json:"uri,omitempty"`	
}

type JSearchResultUrls struct{
	Url			string	`json:"url"`
	Title		string	`json:"title"`
	Description	string	`json:"description"`
}

type JSearchResult struct{
	Enrichment 				int 					`json:"enrichment"`
	BrandHuntingRuleIds 	[]string				`json:"brandHuntingRuleIds"`
	Urls					[]JSearchResultUrls	`json:"urls"`
}

type JResultPrice struct{
	Amount		float32 	`json:"amount"`
	Currency	string		`json:"currency"`
}

type JResultLink struct{
	Url		string	`json:"url"`
}

type JEnrichFields struct{
	Title		string 			`json:"title,omitempty"`
	Description	string 			`json:"description,omitempty"`
	Status		string 			`json:"status,omitempty"`
	Detected	string 			`json:"detected,omitempty"`
	IsAd		string 			`json:"isAd,omitempty"`
	Sales		string 			`json:"sales,omitempty"`
	Price		JResultPrice 	`json:"price,omitempty"`
	Stars		string 			`json:"stars,omitempty"`
	Links		[]JResultLink	`json:"links,omitempty"`
	Uri			string			`json:"uri,omitempty"`
}

type JResultFile struct{
	Name		string	`json:"name,omitempty"`
	Sha256		string	`json:"sha256"`
	Mimetype	string	`json:"mimetype"`
	Type		string	`json:"type,omitempty"`
	File		*string	`json:"file,omitempty"`
}
type JEnrichResult struct {
	SearchResult 	JEnrichFields	`json:"searchResult"`
	Files			[]*JResultFile	`json:"files"`
}

type JTaskResult struct{
	JTask
	Event			string					`json:"event,omitempty"`
	Error			string					`json:"error,omitempty"`
	Unexpected		string					`json:"unexpected,omitempty"`
	Urls			[]JSearchResultUrls	`json:"urls,omitempty"`
	Proto 			[]string				`json:"proto,omitempty"`
	SearchResults 	[]JEnrichResult		`json:"searchResults,omitempty"`
	Files			[]*JResultFile			`json:"files,omitempty"`
	Chunk			bool					`json:"chunk,omitempty"`
}

type AccRegion struct{
	Country		string	`json:"country,omitempty"`
	City		string	`json:"city,omitempty"`
}

