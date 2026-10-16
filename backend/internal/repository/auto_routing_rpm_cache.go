package repository

import (
	"context"
	"fmt"
)

const autoRoutingRPMAdmissionScript = `
if redis.call('EXISTS', KEYS[3]) == 1 then return 0 end
local user_limit = tonumber(ARGV[1])
local group_limit = tonumber(ARGV[2])
if user_limit > 0 and tonumber(redis.call('GET', KEYS[1]) or '0') >= user_limit then return 2 end
if group_limit > 0 and tonumber(redis.call('GET', KEYS[2]) or '0') >= group_limit then return 1 end
if user_limit > 0 then redis.call('INCR', KEYS[1]); redis.call('EXPIRE', KEYS[1], 120) end
if group_limit > 0 then redis.call('INCR', KEYS[2]); redis.call('EXPIRE', KEYS[2], 120) end
redis.call('SET', KEYS[3], '1', 'EX', 86400)
return 0
`

func (c *userRPMCacheImpl) AdmitAutoRoutingRPM(ctx context.Context, userID, groupID, executionID int64, userLimit, groupLimit int) (int, error) {
	minute, err := c.minuteTS(ctx)
	if err != nil {
		return 0, err
	}
	keys := []string{
		fmt.Sprintf("%s%d:%d", userRPMKeyPrefix, userID, minute),
		fmt.Sprintf("%s%d:%d:%d", userGroupRPMKeyPrefix, userID, groupID, minute),
		fmt.Sprintf("rpm:auto:%d:%d", userID, executionID),
	}
	return c.rdb.Eval(ctx, autoRoutingRPMAdmissionScript, keys, userLimit, groupLimit).Int()
}
