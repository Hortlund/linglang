#define _POSIX_C_SOURCE 200809L
#include <sqlite3.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <math.h>
#include <pthread.h>
#include <signal.h>
#include <sys/time.h>
#include <unistd.h>

/* Private length-prefixed protocol; all input/output is capped at 8 MiB.
 * A separate process keeps the BEAM scheduler independent of SQLite work. */
#define LIMIT (8u * 1024u * 1024u)
static unsigned char output[LIMIT];
static size_t used, input_used;
static int overflow;
static uint32_t number(void) {
 unsigned char b[4]; if (fread(b,1,4,stdin)!=4) _exit(2);
 return ((uint32_t)b[0]<<24)|((uint32_t)b[1]<<16)|((uint32_t)b[2]<<8)|b[3];
}
static char *field(uint32_t *size) {
 *size=number(); input_used+=4u+(size_t)*size;
 if(input_used>LIMIT) _exit(2);
 char *p=malloc((size_t)*size+1);if(!p) _exit(2);
 if(fread(p,1,*size,stdin)!=*size) _exit(2);
 p[*size]=0;return p;
}
static void bytes(const void *p,size_t n){if(n>LIMIT-used){overflow=1;return;}memcpy(output+used,p,n);used+=n;}
static void byte(unsigned char b){bytes(&b,1);}
static void word(uint32_t n){unsigned char b[4]={(unsigned char)(n>>24),(unsigned char)(n>>16),(unsigned char)(n>>8),(unsigned char)n};bytes(b,4);}
static void text(const void *p,size_t n){word((uint32_t)n);bytes(p,n);}
static int fail(const char *why){used=0;overflow=0;byte(0);text(why,strlen(why));fwrite(output,1,used,stdout);return 0;}
static void expire(int signal_number){(void)signal_number;_exit(124);}
static void *lifetime(void *unused){
 (void)unused;
 /* Do not hold stdin's FILE lock: glibc exit cleanup needs that lock while
  * the parent deliberately keeps the pipe open until this helper exits. */
 unsigned char byte;
 ssize_t count;
 do { count=read(STDIN_FILENO,&byte,1); } while(count<0 && errno==EINTR);
 _exit(count==0?130:2);
}
int main(void){
 uint32_t ms=number(), pn, sn, count;char *path=field(&pn),*sql=field(&sn);
 if(strlen(path)!=pn||strlen(sql)!=sn){free(path);free(sql);return fail("NUL in database path or SQL");}
 count=number();if(count>32766){free(path);free(sql);return fail("too many SQL parameters");}
 char **kinds=calloc(count?count:1,sizeof(char*)),**values=calloc(count?count:1,sizeof(char*));
 uint32_t *sizes=calloc(count?count:1,sizeof(uint32_t));if(!kinds||!values||!sizes)_exit(2);
 for(uint32_t i=0;i<count;i++){uint32_t n;kinds[i]=field(&n);if(strlen(kinds[i])!=n)_exit(2);values[i]=field(&sizes[i]);}
 pthread_t guard;if(pthread_create(&guard,NULL,lifetime,NULL)!=0)_exit(2);
 if(ms){signal(SIGALRM,expire);struct itimerval timer={{0,0},{ms/1000,(ms%1000)*1000}};if(setitimer(ITIMER_REAL,&timer,NULL)!=0)_exit(2);}
 sqlite3 *db=NULL;sqlite3_stmt *statement=NULL,*extra=NULL;const char *tail=NULL;char error[1024]="";
 int rc=sqlite3_open_v2(path,&db,SQLITE_OPEN_READWRITE|SQLITE_OPEN_CREATE,NULL);
 if(rc!=SQLITE_OK)goto failure;
 sqlite3_limit(db,SQLITE_LIMIT_LENGTH,LIMIT);sqlite3_limit(db,SQLITE_LIMIT_SQL_LENGTH,LIMIT);
 sqlite3_busy_timeout(db,ms&&ms<5000?(int)ms:5000);
 rc=sqlite3_prepare_v2(db,sql,(int)sn+1,&statement,&tail);if(rc!=SQLITE_OK)goto failure;
 if(!statement){snprintf(error,sizeof(error),"expected one SQL statement");goto failure;}
 rc=sqlite3_prepare_v2(db,tail,-1,&extra,NULL);
 if(rc!=SQLITE_OK||extra){snprintf(error,sizeof(error),"expected exactly one SQL statement");goto failure;}
 if(sqlite3_bind_parameter_count(statement)!=(int)count){snprintf(error,sizeof(error),"SQL parameter count mismatch");goto failure;}
 for(uint32_t i=0;i<count;i++){
  char *kind=kinds[i],*v=values[i],*end=NULL;int index=(int)i+1;
  if(!*kind||!strcmp(kind,"text"))rc=sqlite3_bind_text(statement,index,v,(int)sizes[i],SQLITE_STATIC);
  else if(!strcmp(kind,"blob"))rc=sqlite3_bind_blob(statement,index,v,(int)sizes[i],SQLITE_STATIC);
  else if(!strcmp(kind,"null"))rc=sqlite3_bind_null(statement,index);
  else if(!strcmp(kind,"int")){errno=0;long long n=strtoll(v,&end,10);if(errno||!*v||end!=v+sizes[i]){snprintf(error,sizeof(error),"invalid int parameter");goto failure;}rc=sqlite3_bind_int64(statement,index,n);}
  else if(!strcmp(kind,"float")){errno=0;double n=strtod(v,&end);if(errno||!*v||end!=v+sizes[i]||!isfinite(n)){snprintf(error,sizeof(error),"invalid float parameter");goto failure;}rc=sqlite3_bind_double(statement,index,n);}
  else{snprintf(error,sizeof(error),"invalid SQL value kind");goto failure;}
  if(rc!=SQLITE_OK)goto failure;
 }
 int cols=sqlite3_column_count(statement);byte(1);word((uint32_t)cols);
 for(int i=0;i<cols;i++){const char *name=sqlite3_column_name(statement,i);text(name,strlen(name));}
 while((rc=sqlite3_step(statement))==SQLITE_ROW){
  byte(1);
  for(int i=0;i<cols;i++){
   int type=sqlite3_column_type(statement,i);unsigned char kind=0;const void *value="";int n=0;
   if(type!=SQLITE_NULL){kind=type==SQLITE_INTEGER?1:type==SQLITE_FLOAT?2:type==SQLITE_TEXT?3:4;value=type==SQLITE_BLOB?sqlite3_column_blob(statement,i):sqlite3_column_text(statement,i);n=sqlite3_column_bytes(statement,i);}
   byte(kind);text(n?value:"",(size_t)n);
  }
  if(overflow){snprintf(error,sizeof(error),"SQLite output exceeds 8 MiB");goto failure;}
 }
 if(rc!=SQLITE_DONE)goto failure;
 byte(0);uint64_t changes=(uint64_t)sqlite3_changes64(db);for(int i=7;i>=0;i--)byte((unsigned char)(changes>>(i*8)));
 if(overflow){snprintf(error,sizeof(error),"SQLite output exceeds 8 MiB");goto failure;}
 sqlite3_finalize(statement);sqlite3_finalize(extra);sqlite3_close(db);
 fwrite(output,1,used,stdout);return 0;
failure:
 if(!*error)snprintf(error,sizeof(error),"%s",db?sqlite3_errmsg(db):"cannot open SQLite");
 sqlite3_finalize(statement);sqlite3_finalize(extra);sqlite3_close(db);
 return fail(error);
}
