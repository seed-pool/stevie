package sportsdb

import "strings"

// homeVenue maps EPG "From … in City" venue fragments to home team + sport.
var homeVenues = []struct {
	needle string
	sport  string
	home   string
}{
	// NHL
	{"bell centre", "Ice Hockey", "Montreal Canadiens"},
	{"centre bell", "Ice Hockey", "Montreal Canadiens"},
	{"scotiabank arena", "Ice Hockey", "Toronto Maple Leafs"},
	{"little caesars arena", "Ice Hockey", "Detroit Red Wings"},
	{"madison square garden", "Ice Hockey", "New York Rangers"},
	{"lenovo center", "Ice Hockey", "Carolina Hurricanes"},
	{"climate pledge arena", "Ice Hockey", "Seattle Kraken"},
	{"ball arena", "Ice Hockey", "Colorado Avalanche"}, // also Nuggets — disambiguate by sport context
	{"delta center", "Ice Hockey", "Utah Mammoth"},
	// NBA
	{"spectrum center", "Basketball", "Charlotte Hornets"},
	{"ball arena", "Basketball", "Denver Nuggets"},
	{"delta center", "Basketball", "Utah Jazz"},
	{"crypto.com arena", "Basketball", "Los Angeles Lakers"},
	{"chase center", "Basketball", "Golden State Warriors"},
	{"madison square garden", "Basketball", "New York Knicks"},
	{"td garden", "Basketball", "Boston Celtics"},
	{"american airlines center", "Basketball", "Dallas Mavericks"},
	{"footprint center", "Basketball", "Phoenix Suns"},
	{"smoothie king center", "Basketball", "New Orleans Pelicans"},
	{"frost bank center", "Basketball", "San Antonio Spurs"},
	{"target center", "Basketball", "Minnesota Timberwolves"},
	{"united center", "Basketball", "Chicago Bulls"},
	{"rocket mortgage fieldhouse", "Basketball", "Cleveland Cavaliers"},
	{"little caesars arena", "Basketball", "Detroit Pistons"},
	{"gainbridge fieldhouse", "Basketball", "Indiana Pacers"},
	{"kaseya center", "Basketball", "Miami Heat"},
	{"state farm arena", "Basketball", "Atlanta Hawks"},
	{"capital one arena", "Basketball", "Washington Wizards"},
	{"wells fargo center", "Basketball", "Philadelphia 76ers"},
	{"barclays center", "Basketball", "Brooklyn Nets"},
	{"scotiabank arena", "Basketball", "Toronto Raptors"},
	{"modas center", "Basketball", "Portland Trail Blazers"},
	{"moda center", "Basketball", "Portland Trail Blazers"},
	{"golden 1 center", "Basketball", "Sacramento Kings"},
	{"paycom center", "Basketball", "Oklahoma City Thunder"},
	{"toyota center", "Basketball", "Houston Rockets"},
	{"fedexforum", "Basketball", "Memphis Grizzlies"},
	{"fiserv forum", "Basketball", "Milwaukee Bucks"},
	{"intuit dome", "Basketball", "Los Angeles Clippers"},
	// MLB
	{"petco park", "Baseball", "San Diego Padres"},
	{"american family field", "Baseball", "Milwaukee Brewers"},
	{"dodger stadium", "Baseball", "Los Angeles Dodgers"},
	{"truist park", "Baseball", "Atlanta Braves"},
	{"yankee stadium", "Baseball", "New York Yankees"},
	{"fenway park", "Baseball", "Boston Red Sox"},
	{"wrigley field", "Baseball", "Chicago Cubs"},
	{"oracle park", "Baseball", "San Francisco Giants"},
	{"minute maid park", "Baseball", "Houston Astros"},
	{"progressive field", "Baseball", "Cleveland Guardians"},
	{"guaranteed rate field", "Baseball", "Chicago White Sox"},
	{"tropicana field", "Baseball", "Tampa Bay Rays"},
	{"citizens bank park", "Baseball", "Philadelphia Phillies"},
}

// HomeTeamFromVenueDescription extracts a home team from EPG descriptions like
// "From Spectrum Center in Charlotte, N.C."
func HomeTeamFromVenueDescription(sport, description string) (home string, ok bool) {
	d := strings.ToLower(description)
	if d == "" {
		return "", false
	}
	for _, v := range homeVenues {
		if sport != "" && !strings.EqualFold(v.sport, sport) {
			continue
		}
		if strings.Contains(d, v.needle) {
			return v.home, true
		}
	}
	return "", false
}
