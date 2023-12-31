/*
 * Copyright (C) 2017 The "MysteriumNetwork/node" Authors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package nat

import (
	"github.com/mysteriumnetwork/node/config"
	"github.com/mysteriumnetwork/node/utils/cmdutil"
)

// NewService returns Windows OS specific NAT service based on Internet Connection Sharing (ICS).
func NewService() NATService {
	if config.GetBool(config.FlagUserspace) {
		return &serviceNoop{}
	}

	return &serviceICS{
		setICSAddresses: setICSAddresses,
		powerShell:      cmdutil.PowerShell,
	}
}
