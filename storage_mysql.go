package cachewb

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"reflect"
	"strconv"
	"strings"
)

type ConfigMysql struct {
	Host              string
	Username          string
	Password          string
	Port              int
	DBName            string
	MaxOpenConnection int
}

type mySQL struct {
	mysqlDB              *sql.DB
	itemTemplate         interface{}
	fieldsMap            map[string]string
	tableName            string
	isView               bool
	viewQuery            string
	uniqueIdentity       string
	selectQueryPre       string
	updateQuery          string
	updateQueryFields    []string
	insertManyQueryPart1 string
	insertManyQueryPart2 string
	insertQueryFields    []string
	whereFieldName       []string
	whereUpdateFieldName []string
	cfg                  ConfigMysql
	insertManyLimit      int
	removeManyLimit      int
}

func newMySQL(tableName string, viewQuery string, cfg ConfigMysql, itemTemplate interface{}) *mySQL {
	m := &mySQL{cfg: cfg, tableName: tableName}
	if viewQuery != "" {
		m.isView = true
		m.viewQuery = viewQuery
	}
	m.itemTemplate = itemTemplate
	m.parseTemplate()
	m.insertManyLimit = 1000
	m.removeManyLimit = 1000
	return m
}

func (c *mySQL) getInsertLimit() int {
	return c.insertManyLimit
}

func (c *mySQL) parseTemplate() {
	setClause := ""
	selectClause := ""
	whereClause := ""
	whereUpdateClause := ""
	val1 := ""
	val2 := ""
	c.fieldsMap = make(map[string]string)
	t := reflect.TypeOf(c.itemTemplate)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag := f.Tag.Get("storage"); tag != "" {
			c.fieldsMap[tag] = f.Name
			if len(selectClause) > 0 {
				selectClause = fmt.Sprintf("%s, %s", selectClause, tag)
			} else {
				selectClause = fmt.Sprintf("%s", tag)
			}
			if f.Tag.Get("uniqueIdentity") == "1" {
				c.uniqueIdentity = f.Name
				c.whereFieldName = append(c.whereFieldName, f.Name)
				if len(whereClause) > 0 {
					whereClause = fmt.Sprintf("%s and %s = ?", whereClause, tag)
				} else {
					whereClause = fmt.Sprintf("%s = ?", tag)
				}
			} else if f.Tag.Get("update") != "0" && f.Tag.Get("updateKey") != "1" && f.Tag.Get("autoInc") != "1" {
				c.updateQueryFields = append(c.updateQueryFields, f.Name)
				if len(setClause) > 0 {
					setClause = fmt.Sprintf("%s, %s = ?", setClause, tag)
				} else {
					setClause = fmt.Sprintf("%s = ?", tag)
				}
			}
			if f.Tag.Get("updateKey") == "1" {
				//whereFieldName = f.Name
				c.whereUpdateFieldName = append(c.whereUpdateFieldName, f.Name)
				if len(whereUpdateClause) > 0 {
					whereUpdateClause = fmt.Sprintf("%s and %s = ?", whereUpdateClause, tag)
				} else {
					whereUpdateClause = fmt.Sprintf("%s = ?", tag)
				}
			}
			if f.Tag.Get("insert") != "0" {
				if f.Tag.Get("autoInc") != "1" {
					c.insertQueryFields = append(c.insertQueryFields, f.Name)
					if len(val1) > 0 {
						val1 = fmt.Sprintf("%s, %s", val1, tag)
						val2 = fmt.Sprintf("%s, ?", val2)
					} else {
						val1 = fmt.Sprintf("%s", tag)
						val2 = fmt.Sprintf("?")
					}
				}
			}
		}
	}

	if len(c.whereFieldName) == 0 {
		panic("Can't find Key") // TODO fix message
	}
	// If not exist tag "updateKey", then tag "uniqueIdentity" used in update
	if len(c.whereUpdateFieldName) == 0 {
		c.whereUpdateFieldName = c.whereFieldName
		whereUpdateClause = whereClause
	}
	c.viewQuery = strings.TrimRight(strings.TrimRight(c.viewQuery, " "), ";")
	
	c.updateQueryFields = append(c.updateQueryFields, c.whereUpdateFieldName...)

	c.selectQueryPre = fmt.Sprintf("SELECT %s FROM %s", selectClause, c.tableName)
	c.updateQuery = fmt.Sprintf("UPDATE %s SET %s WHERE %s;", c.tableName, setClause, whereUpdateClause)
	c.insertManyQueryPart1 = fmt.Sprintf("INSERT INTO %s (%s) values ", c.tableName, val1)
	c.insertManyQueryPart2 = fmt.Sprintf("(%s)", val2)

	//fmt.Println(c.selectQueryPre)
	//fmt.Println(c.updateQuery)
	//fmt.Println(c.updateQueryFields)
	//fmt.Println(c.insertQueryFields)
}

func (c *mySQL) checkConnection() {
	if c.mysqlDB == nil {
		qs := c.cfg.Username + ":" + c.cfg.Password + "@tcp(" + c.cfg.Host + ":" + strconv.Itoa(c.cfg.Port) + ")/" + c.cfg.DBName + "?parseTime=true"
		var err error
		c.mysqlDB, err = sql.Open("mysql", qs)
		if err != nil {
			panic(err.Error()) // Just for example purpose. You should use proper error handling instead of panic
		}
		c.mysqlDB.SetMaxOpenConns(c.cfg.MaxOpenConnection)
	}
}

func (c *mySQL) get(keys []string, values[]interface{}) ([]interface{}, error) {
	whereClause := ""
	for _, a := range keys {
		if len(whereClause) > 0 {
			whereClause = fmt.Sprintf("%s and %s = ?", whereClause, a)
		} else {
			whereClause = fmt.Sprintf("%s = ?", a)
		}
	}
	q := ""
	if c.isView{
		if len(whereClause) > 0{
			q = fmt.Sprintf("%s where %s;", c.viewQuery, whereClause)
		}else{
			q = c.viewQuery
		}
	}else {
		if len(whereClause) > 0 {
			q = fmt.Sprintf("%s where %s;", c.selectQueryPre, whereClause)
		}else{
			q = c.selectQueryPre
		}
	}
	//fmt.Println(q)
	var resArr []interface{}
	c.checkConnection()

	stmt, err := c.mysqlDB.Prepare(q)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	rows, err := stmt.Query(values...)
	if err != nil {
		panic(err)
	}

	columns, _ := rows.Columns()
	count := len(columns)
	resValues := make([]interface{}, count)
	resValuePtrs := make([]interface{}, count)

	for rows.Next() {
		val := reflect.New(reflect.TypeOf(c.itemTemplate))
		elem := val.Elem()
		for i := range columns {
			resValuePtrs[i] = &resValues[i]
		}
		rows.Scan(resValuePtrs...)
		//fmt.Println(resValues)
		for i, col := range columns {
			val := resValues[i]
			resByte, okByte := val.([]byte)
			if elem.Kind() == reflect.Struct {
				if c2, ok := c.fieldsMap[col]; ok {
					f := elem.FieldByName(c2)
					if f.IsValid() && f.CanSet() {
						//fmt.Println("##### Consider me ", c2, f.Kind(), reflect.TypeOf(val), val)
						if f.Kind() == reflect.Float64 {
							if okByte {
								//(float64) supported mysql data types : decimal
								r, _ := strconv.ParseFloat(string(resByte), 64)
								f.SetFloat(r)
							} else {
								//(float64) supported mysql data types : double, real
								f.Set(reflect.ValueOf(val))
							}
						} else if f.Kind() == reflect.Slice {
							// ([]byte) supported mysql data types : binary, tinyblob
							f.Set(reflect.ValueOf(val))
						} else if f.Kind() == reflect.String {
							// (string) supported mysql data types :varchar, varbinary, tinytext
							if okByte {
								f.Set(reflect.ValueOf(string(resByte)))
							}
						} else if val != nil{
							//fmt.Println(c2, val)
							f.Set(reflect.ValueOf(val))
						}
					}
				}
			}
		}
		resArr = append(resArr, val.Interface())
	}
	return resArr, nil
}

func (c *mySQL) getBySquirrel(squirrelArgs ...interface{}) ([]interface{}, error){

	q := ""
	if c.isView{
		q = fmt.Sprintf("%s where %s;", c.viewQuery, squirrelArgs[0])
	}else {
		q = fmt.Sprintf("%s where %s;", c.selectQueryPre, squirrelArgs[0])
	}
	//fmt.Println(len(squirrelArgs[1].([]interface{})))
	//fmt.Println(q)
	var resArr []interface{}
	c.checkConnection()

	stmt, err := c.mysqlDB.Prepare(q)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	rows, err := stmt.Query(squirrelArgs[1].([]interface{})...)
	if err != nil {
		panic(err)
	}

	columns, _ := rows.Columns()
	count := len(columns)
	resValues := make([]interface{}, count)
	resValuePtrs := make([]interface{}, count)

	for rows.Next() {
		val := reflect.New(reflect.TypeOf(c.itemTemplate))
		elem := val.Elem()
		for i := range columns {
			resValuePtrs[i] = &resValues[i]
		}
		rows.Scan(resValuePtrs...)
		//fmt.Println(resValues)
		for i, col := range columns {
			val := resValues[i]
			resByte, okByte := val.([]byte)
			if elem.Kind() == reflect.Struct {
				if c2, ok := c.fieldsMap[col]; ok {
					f := elem.FieldByName(c2)
					if f.IsValid() && f.CanSet() {
						//fmt.Println("##### Consider me ", c2, f.Kind(), reflect.TypeOf(val), val)
						if f.Kind() == reflect.Float64 {
							if okByte {
								//(float64) supported mysql data types : decimal
								r, _ := strconv.ParseFloat(string(resByte), 64)
								f.SetFloat(r)
							} else {
								//(float64) supported mysql data types : double, real
								f.Set(reflect.ValueOf(val))
							}
						} else if f.Kind() == reflect.Slice {
							// ([]byte) supported mysql data types : binary, tinyblob
							f.Set(reflect.ValueOf(val))
						} else if f.Kind() == reflect.String {
							// (string) supported mysql data types :varchar, varbinary, tinytext
							if okByte {
								f.Set(reflect.ValueOf(string(resByte)))
							}
						} else if val != nil{
							//fmt.Println(c2, val)
							f.Set(reflect.ValueOf(val))
						}
					}
				}
			}
		}
		resArr = append(resArr, val.Interface())
	}
	return resArr, nil
}

func (c *mySQL) update(in interface{}) (map[string]int64, error) {
	if c.isView {
		return nil, errors.New("view does not support update")
	}

	elem := reflect.ValueOf(in).Elem()

	valuePtrs := make([]interface{}, 0)

	for _, n := range c.updateQueryFields {
		zz := elem.FieldByName(n)
		if zz.IsValid() {
			valuePtrs = append(valuePtrs, zz.Interface())
		}
	}
	c.checkConnection()
	stmt, err := c.mysqlDB.Prepare(c.updateQuery)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	res, err := stmt.Exec(valuePtrs...)
	if err != nil {
		panic(err)
	}
	m := make(map[string]int64)
	m["LastInsertId"], _ = res.LastInsertId()
	m["RowsAffected"], _ = res.RowsAffected()
	return m, nil
}

// args is array of items
// It able insert to support single and multi insert together
func (c *mySQL) insert(args ...interface{}) (map[string]int64, error) {
	if c.isView {
		return nil, errors.New("view does not support insert")
	}
	if len(args) > c.insertManyLimit {
		return nil, errors.New(fmt.Sprintf("unable to insert more than limit %d, got %d", c.insertManyLimit, len(args)))
	}
	valuePtrs := make([]interface{}, 0)
	var valueStrs []string

	for _, d := range args {
		valueStrs = append(valueStrs, c.insertManyQueryPart2)
		elem := reflect.ValueOf(d)
		for _, n := range c.insertQueryFields {
			zz := elem.FieldByName(n)
			if zz.IsValid() {
				valuePtrs = append(valuePtrs, zz.Interface())
			}
		}
	}

	c.checkConnection()
	q := fmt.Sprintf("%s %s", c.insertManyQueryPart1, strings.Join(valueStrs, ","))
	stmt, err := c.mysqlDB.Prepare(q)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	res, err := stmt.Exec(valuePtrs...)
	if err != nil {
		panic(err)
	}
	m := make(map[string]int64)
	m["LastInsertId"], _ = res.LastInsertId()
	m["RowsAffected"], _ = res.RowsAffected()
	return m, nil
}

func (c *mySQL) removeByUniqueIdentity(args ...interface{}) (map[string]int64, error) {
	if c.isView {
		return nil, errors.New("view does not support removeByUniqueIdentity")
	}

	if len(args) == 0 {
		return nil, errors.New(fmt.Sprintf("expected at least 1 argument, got %d", len(args)))
	}
	c.checkConnection()
	q := ""
	if len(args) == 1 {
		q = fmt.Sprintf("DELETE FROM %s WHERE %s = ?;", c.tableName, c.uniqueIdentity)
	}else{
		if len(args) > c.removeManyLimit {
			return nil, errors.New(fmt.Sprintf("unable to removeByUniqueIdentity more than limit %d, got %d", c.insertManyLimit, len(args)))
		}
		var a []string
		for range args{
			a = append(a, "?")
		}
		q = fmt.Sprintf("DELETE FROM %s WHERE %s in (%s);", c.tableName, c.uniqueIdentity, strings.Join(a, ", "))
	}
	fmt.Println(q)
	//return nil, nil
	stmt, err := c.mysqlDB.Prepare(q)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	res, err := stmt.Exec(args...)
	if err != nil {
		panic(err)
	}
	m := make(map[string]int64)
	m["LastInsertId"], _ = res.LastInsertId()
	m["RowsAffected"], _ = res.RowsAffected()
	return m, nil
}

func (c *mySQL) remove(keys []string, values[]interface{}) (map[string]int64, error) {
	if c.isView {
		return nil, errors.New("view does not support removeByUniqueIdentity")
	}

	if len(keys) == 0 {
		return nil, errors.New(fmt.Sprintf("expected at least 1 argument, got %d", len(keys)))
	}

	whereClause := ""
	for _, a := range keys {
		if len(whereClause) > 0 {
			whereClause = fmt.Sprintf("%s and %s = ?", whereClause, a)
		} else {
			whereClause = fmt.Sprintf("%s = ?", a)
		}
	}
	c.checkConnection()
	q := fmt.Sprintf("DELETE from %s where %s;", c.tableName, whereClause)
	//fmt.Println(q)
	stmt, err := c.mysqlDB.Prepare(q)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	res, err := stmt.Exec(values...)
	if err != nil {
		panic(err)
	}
	m := make(map[string]int64)
	m["LastInsertId"], _ = res.LastInsertId()
	m["RowsAffected"], _ = res.RowsAffected()
	return m, nil
}

//func (c *mySQL) updateBatch(args ...interface{}) (map[string]int64, error) {
//	if c.isView {
//		return nil, errors.New("view does not support update")
//	}
//	if len(args) > 10 {
//		return nil, errors.New(fmt.Sprintf("unable to update more than limit %d, got %d", 10, len(args)))
//	}
//	valuePtrs := make([]interface{}, 0)
//	myId := "id"
//	var ararar  []string
//	for _, n := range c.updateQueryFields {
//		if n != "Id" { // todo
//			aaaa := fmt.Sprintf("%s = CASE %s ", c.fieldsMapNameToTag[n], myId)
//			for range args {
//				aaaa = fmt.Sprintf("%s WHEN ? THEN ? ", aaaa)
//			}
//			aaaa = fmt.Sprintf("%s END", aaaa)
//			ararar = append(ararar, aaaa)
//			for _, item := range args {
//				elem := reflect.ValueOf(item).Elem()
//				zz := elem.FieldByName("Id")
//				if zz.IsValid() {
//					valuePtrs = append(valuePtrs, zz.Interface())
//				}
//				zz = elem.FieldByName(n)
//				if zz.IsValid() {
//					valuePtrs = append(valuePtrs, zz.Interface())
//				}
//			}
//		}
//	}
//
//	var ararar2  []string
//	for _, item := range args {
//		elem := reflect.ValueOf(item).Elem()
//		zz := elem.FieldByName("Id")
//		if zz.IsValid() {
//			valuePtrs = append(valuePtrs, zz.Interface())
//		}
//		ararar2 = append(ararar2, "?")
//	}
//
//	qqq := fmt.Sprintf("Update members set %s where %s in (%s)", strings.Join(ararar, ", "), myId, strings.Join(ararar2, ", "))
//	//fmt.Println(qqq)
//	c.checkConnection()
//	stmt, err := c.mysqlDB.Prepare(qqq)
//	if err != nil {
//		panic(err)
//	}
//	defer stmt.Close()
//	res, err := stmt.Exec(valuePtrs...)
//	if err != nil {
//		panic(err)
//	}
//	m := make(map[string]int64)
//	m["LastInsertId"], _ = res.LastInsertId()
//	m["RowsAffected"], _ = res.RowsAffected()
//	return m, nil
//}
