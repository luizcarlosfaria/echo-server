using System;
using System.IO;
using System.Collections.Generic;
using System.Dynamic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Authorization;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Mvc;
using Microsoft.Extensions.Primitives;
using Newtonsoft.Json.Linq;
using System.Text;
using Newtonsoft.Json;

namespace Jornada.Controllers
{
    public class DefaultController : Controller
    {
        public DefaultController()
        {
        }

        private bool Accept(string format) => this.Request.Headers["Accept"].FirstOrDefault()?.Contains(format) ?? false;


        public async Task<IActionResult> Index()
        {
            if (this.Accept("text/html"))
                return this.IndexHtml();
            else if (this.Accept("application/json") || this.Accept("*/*"))
                return await this.IndexJsonAsync();
            else
                return this.View();
        }

        public IActionResult IndexHtml()
        {
            return this.View("Index");
        }

        string[] verbsWithForms = new string[] { "POST", "PUT" };
        string[] contentTypeWithoutForms = new string[] { "application/json", null };
        string[] contentJson = new string[] { "application/json" };

        public async Task<IActionResult> IndexJsonAsync()
        {
            JObject returnValue = new JObject();
            returnValue.Add("Headers", JObject.FromObject(this.Request.Headers));
            returnValue.Add("Path", new JValue(this.Request.Path));
            returnValue.Add("MachineName", new JValue(Environment.MachineName));
            returnValue.Add("Method", new JValue(this.Request.Method));
            returnValue.Add("Scheme", new JValue(this.Request.Scheme));
            returnValue.Add("QueryString", JArray.FromObject(this.Request.Query));
            returnValue.Add("Forms", JArray.FromObject(this.verbsWithForms.Contains(this.Request.Method)
                                && this.contentTypeWithoutForms.Contains(this.Request.ContentType)
                                    ? new FormCollection(null, null) : this.Request.Form));
            await this.SetPayload(returnValue);
            returnValue.Add("EnvironmentVariables", JObject.FromObject(Environment.GetEnvironmentVariables()));

            string returnJson = returnValue.ToString();

            return this.Content(returnJson, this.contentJson.First());
        }

        private async Task SetPayload(JObject returnValue)
        {
            if (this.contentJson.Contains(this.Request.ContentType))
            {
                this.Request.EnableBuffering();
                var buffer = new byte[Convert.ToInt32(this.Request.ContentLength)];
                await this.Request.Body.ReadAsync(buffer, 0, buffer.Length);
                string json = Encoding.UTF8.GetString(buffer);
                returnValue.Add("Payload", JObject.Parse(json));
            }
        }
    }
}
