package cockroachsql

import (
	"context"
	"sync"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// operationLock serializes resource writes against each other and against
// reads. Writes may temporarily grant a role to the connected user and revoke
// it afterwards (see withRolesGranted). When Terraform applies resources in
// parallel, one write can revoke a membership another write still needs, and
// CockroachDB can fail concurrent queries by the same user with
// "role/user ... does not exist" while the membership changes. Root is a
// member of every role and never needs these grants, so this only shows up
// for non-root admin users.
var operationLock sync.RWMutex

// serializeWrites wraps the CRUD functions of every resource and data source
// so that create, update and delete run one at a time with no read in
// flight, while reads still run in parallel with each other.
func serializeWrites(p *schema.Provider) {
	for _, r := range p.ResourcesMap {
		r.CreateContext = withLock(r.CreateContext, false)
		r.UpdateContext = withLock(r.UpdateContext, false)
		r.DeleteContext = withLock(r.DeleteContext, false)
		r.ReadContext = withLock(r.ReadContext, true)
	}
	for _, r := range p.DataSourcesMap {
		r.ReadContext = withLock(r.ReadContext, true)
	}
}

func withLock[F ~func(context.Context, *schema.ResourceData, any) diag.Diagnostics](fn F, read bool) F {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		if read {
			operationLock.RLock()
			defer operationLock.RUnlock()
		} else {
			operationLock.Lock()
			defer operationLock.Unlock()
		}
		return fn(ctx, d, meta)
	}
}
