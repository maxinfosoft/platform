// Copyright (c) 2015 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package store

import (
	l4g "github.com/alecthomas/log4go"
	"github.com/mattermost/platform/model"
	"github.com/mattermost/platform/utils"
)

type SqlSessionStore struct {
	*SqlStore
}

func NewSqlSessionStore(sqlStore *SqlStore) SessionStore {
	us := &SqlSessionStore{sqlStore}

	for _, db := range sqlStore.GetAllConns() {
		table := db.AddTableWithName(model.Session{}, "Sessions").SetKeys(false, "Id")
		table.ColMap("Id").SetMaxSize(26)
		table.ColMap("Token").SetMaxSize(26)
		table.ColMap("UserId").SetMaxSize(26)
		table.ColMap("DeviceId").SetMaxSize(512)
		table.ColMap("Roles").SetMaxSize(64)
		table.ColMap("Props").SetMaxSize(1000)
	}

	return us
}

func (me SqlSessionStore) CreateIndexesIfNotExists() {
	me.CreateIndexIfNotExists("idx_sessions_user_id", "Sessions", "UserId")
	me.CreateIndexIfNotExists("idx_sessions_token", "Sessions", "Token")
	me.CreateIndexIfNotExists("idx_sessions_expires_at", "Sessions", "ExpiresAt")
	me.CreateIndexIfNotExists("idx_sessions_create_at", "Sessions", "CreateAt")
	me.CreateIndexIfNotExists("idx_sessions_last_activity_at", "Sessions", "LastActivityAt")
}

func (me SqlSessionStore) Save(session *model.Session) StoreChannel {

	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		if len(session.Id) > 0 {
			result.Err = model.NewLocAppError("SqlSessionStore.Save", "store.sql_session.save.existing.app_error", nil, "id="+session.Id)
			storeChannel <- result
			close(storeChannel)
			return
		}

		session.PreSave()

		if cur := <-me.CleanUpExpiredSessions(session.UserId); cur.Err != nil {
			l4g.Error(utils.T("store.sql_session.save.cleanup.error"), cur.Err)
		}

		tcs := me.Team().GetTeamsForUser(session.UserId)

		if err := me.GetMaster().Insert(session); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.Save", "store.sql_session.save.app_error", nil, "id="+session.Id+", "+err.Error())
			return
		} else {
			result.Data = session
		}

		if rtcs := <-tcs; rtcs.Err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.Save", "store.sql_session.save.app_error", nil, "id="+session.Id+", "+rtcs.Err.Error())
			return
		} else {
			tempMembers := rtcs.Data.([]*model.TeamMember)
			session.TeamMembers = make([]*model.TeamMember, 0, len(tempMembers))
			for _, tm := range tempMembers {
				if tm.DeleteAt == 0 {
					session.TeamMembers = append(session.TeamMembers, tm)
				}
			}
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) Get(sessionIdOrToken string) StoreChannel {

	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		var sessions []*model.Session

		if _, err := me.GetReplica().Select(&sessions, "SELECT * FROM Sessions WHERE Token = :Token OR Id = :Id LIMIT 1", map[string]interface{}{"Token": sessionIdOrToken, "Id": sessionIdOrToken}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.Get", "store.sql_session.get.app_error", nil, "sessionIdOrToken="+sessionIdOrToken+", "+err.Error())
		} else if sessions == nil || len(sessions) == 0 {
			result.Err = model.NewLocAppError("SqlSessionStore.Get", "store.sql_session.get.app_error", nil, "sessionIdOrToken="+sessionIdOrToken)
		} else {
			result.Data = sessions[0]

			tcs := me.Team().GetTeamsForUser(sessions[0].UserId)
			if rtcs := <-tcs; rtcs.Err != nil {
				result.Err = model.NewLocAppError("SqlSessionStore.Get", "store.sql_session.get.app_error", nil, "sessionIdOrToken="+sessionIdOrToken+", "+rtcs.Err.Error())
				return
			} else {
				tempMembers := rtcs.Data.([]*model.TeamMember)
				sessions[0].TeamMembers = make([]*model.TeamMember, 0, len(tempMembers))
				for _, tm := range tempMembers {
					if tm.DeleteAt == 0 {
						sessions[0].TeamMembers = append(sessions[0].TeamMembers, tm)
					}
				}
			}
		}

		storeChannel <- result
		close(storeChannel)

	}()

	return storeChannel
}

func (me SqlSessionStore) GetSessions(userId string) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {

		if cur := <-me.CleanUpExpiredSessions(userId); cur.Err != nil {
			l4g.Error(utils.T("store.sql_session.get_sessions.error"), cur.Err)
		}

		result := StoreResult{}
		var sessions []*model.Session

		tcs := me.Team().GetTeamsForUser(userId)

		if _, err := me.GetReplica().Select(&sessions, "SELECT * FROM Sessions WHERE UserId = :UserId ORDER BY LastActivityAt DESC", map[string]interface{}{"UserId": userId}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.GetSessions", "store.sql_session.get_sessions.app_error", nil, err.Error())
		} else {

			result.Data = sessions
		}

		if rtcs := <-tcs; rtcs.Err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.GetSessions", "store.sql_session.get_sessions.app_error", nil, rtcs.Err.Error())
			return
		} else {
			for _, session := range sessions {
				tempMembers := rtcs.Data.([]*model.TeamMember)
				session.TeamMembers = make([]*model.TeamMember, 0, len(tempMembers))
				for _, tm := range tempMembers {
					if tm.DeleteAt == 0 {
						session.TeamMembers = append(session.TeamMembers, tm)
					}
				}
			}
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) Remove(sessionIdOrToken string) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		_, err := me.GetMaster().Exec("DELETE FROM Sessions WHERE Id = :Id Or Token = :Token", map[string]interface{}{"Id": sessionIdOrToken, "Token": sessionIdOrToken})
		if err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.RemoveSession", "store.sql_session.remove.app_error", nil, "id="+sessionIdOrToken+", err="+err.Error())
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) RemoveAllSessions() StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		_, err := me.GetMaster().Exec("DELETE FROM Sessions")
		if err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.RemoveAllSessions", "store.sql_session.remove_all_sessions_for_team.app_error", nil, err.Error())
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) PermanentDeleteSessionsByUser(userId string) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		_, err := me.GetMaster().Exec("DELETE FROM Sessions WHERE UserId = :UserId", map[string]interface{}{"UserId": userId})
		if err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.RemoveAllSessionsForUser", "store.sql_session.permanent_delete_sessions_by_user.app_error", nil, "id="+userId+", err="+err.Error())
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) CleanUpExpiredSessions(userId string) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		if _, err := me.GetMaster().Exec("DELETE FROM Sessions WHERE UserId = :UserId AND ExpiresAt != 0 AND :ExpiresAt > ExpiresAt", map[string]interface{}{"UserId": userId, "ExpiresAt": model.GetMillis()}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.CleanUpExpiredSessions", "store.sql_session.cleanup_expired_sessions.app_error", nil, err.Error())
		} else {
			result.Data = userId
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) UpdateLastActivityAt(sessionId string, time int64) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		if _, err := me.GetMaster().Exec("UPDATE Sessions SET LastActivityAt = :LastActivityAt WHERE Id = :Id", map[string]interface{}{"LastActivityAt": time, "Id": sessionId}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.UpdateLastActivityAt", "store.sql_session.update_last_activity.app_error", nil, "sessionId="+sessionId)
		} else {
			result.Data = sessionId
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) UpdateRoles(userId, roles string) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}
		if _, err := me.GetMaster().Exec("UPDATE Sessions SET Roles = :Roles WHERE UserId = :UserId", map[string]interface{}{"Roles": roles, "UserId": userId}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.UpdateRoles", "store.sql_session.update_roles.app_error", nil, "userId="+userId)
		} else {
			result.Data = userId
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) UpdateDeviceId(id string, deviceId string, expiresAt int64) StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}
		if _, err := me.GetMaster().Exec("UPDATE Sessions SET DeviceId = :DeviceId, ExpiresAt = :ExpiresAt WHERE Id = :Id", map[string]interface{}{"DeviceId": deviceId, "Id": id, "ExpiresAt": expiresAt}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.UpdateDeviceId", "store.sql_session.update_device_id.app_error", nil, err.Error())
		} else {
			result.Data = deviceId
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}

func (me SqlSessionStore) AnalyticsSessionCount() StoreChannel {
	storeChannel := make(StoreChannel, 1)

	go func() {
		result := StoreResult{}

		query :=
			`SELECT
                COUNT(*)
            FROM
                Sessions
            WHERE ExpiresAt > :Time`

		if c, err := me.GetReplica().SelectInt(query, map[string]interface{}{"Time": model.GetMillis()}); err != nil {
			result.Err = model.NewLocAppError("SqlSessionStore.AnalyticsSessionCount", "store.sql_session.analytics_session_count.app_error", nil, err.Error())
		} else {
			result.Data = c
		}

		storeChannel <- result
		close(storeChannel)
	}()

	return storeChannel
}
